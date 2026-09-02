import {
  GOLDEN_IMAGE_NAMESPACE,
  TEMPLATE_NAMESPACE,
  TemplateKind,
  isValidDiskName,
} from './k8s-resources';

/**
 * Flavor/workload suffixes on CNV common-templates
 * (`windows2k19-server-medium`, `windows11-desktop-large`, …).
 * Stripped only for grouping — not used as SKU identity.
 */
const VARIANT_SUFFIXES = [
  'highperformance',
  'desktop',
  'server',
  'xlarge',
  'large',
  'medium',
  'small',
  'tiny',
];

export type WindowsFamily = {
  /** Golden DataVolume / DataSource name (template DATA_SOURCE_NAME). */
  id: string;
  /** Template name prefix after stripping size/workload, e.g. windows2k19. */
  prefix: string;
  displayName: string;
  templates: TemplateKind[];
  goldenNamespace: string;
  diskSize: string;
};

export type IsoHint = {
  /** Direct Microsoft CDN ISO when HEAD verified as a public eval image. Omit when none. */
  url?: string;
  /** Evaluation Center landing page (not an ISO). */
  evalCenter?: string;
  helper: string;
};

export type IsoType = 'eval' | 'consumer';

export type ConsumerEdition = {
  label: string;
  index: number;
};

/**
 * Standard consumer ISO WIM indexes for Win11 25H2 (Win10 uses the same layout).
 * Enterprise is not on standard consumer ISOs — use Enterprise Evaluation instead.
 */
export const CONSUMER_EDITIONS: ConsumerEdition[] = [
  { label: 'Pro', index: 6 },
  { label: 'Home', index: 1 },
  { label: 'Education', index: 4 },
  { label: 'Pro for Workstations', index: 10 },
  { label: 'Pro Education', index: 8 },
];

export const DEFAULT_CONSUMER_EDITION_INDEX = 6;

/**
 * Optional Microsoft Evaluation Center ISO URLs (en-US).
 * Canonical product pages:
 * - https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2025
 * - https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2022
 * - https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2019
 * - https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2016
 * - https://www.microsoft.com/en-us/evalcenter/evaluate-windows-11-enterprise
 *
 * Direct HTTPS hosts are Microsoft CDNs (`software-static.download.prss.microsoft.com`,
 * `software-download.microsoft.com`). HEAD-checked 2026-08-31; paths can rotate.
 * Windows 10/11 client eval has no stable anonymous ISO (registration wall).
 * Never invent a lab ISO.
 */
const ISO_HINTS: Record<string, { url?: string; evalCenter?: string }> = {
  win2k25: {
    url: 'https://software-static.download.prss.microsoft.com/dbazure/998969d5-f34g-4e03-ac9d-1f9786c66749/26100.32230.260111-0550.lt_release_svc_refresh_SERVER_EVAL_x64FRE_en-us.iso',
    evalCenter: 'https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2025',
  },
  win2k22: {
    url: 'https://software-static.download.prss.microsoft.com/sg/download/888969d5-f34g-4e03-ac9d-1f9786c66749/SERVER_EVAL_x64FRE_en-us.iso',
    evalCenter: 'https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2022',
  },
  win2k19: {
    url: 'https://software-download.microsoft.com/download/pr/17763.737.190906-2324.rs5_release_svc_refresh_SERVER_EVAL_x64FRE_en-us_1.iso',
    evalCenter: 'https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2019',
  },
  win2k16: {
    url: 'https://software-download.microsoft.com/download/pr/Windows_Server_2016_Datacenter_EVAL_en-us_14393_refresh.ISO',
    evalCenter: 'https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2016',
  },
  win11: {
    evalCenter: 'https://www.microsoft.com/en-us/evalcenter/evaluate-windows-11-enterprise',
  },
  win10: {
    evalCenter: 'https://www.microsoft.com/en-us/evalcenter',
  },
};

const SUGGEST_HELPER =
  'Suggested Microsoft Evaluation Center ISO (en-US). CDN URLs can expire; clear or replace if this cluster cannot pull it.';

const PASTE_HELPER =
  'Microsoft does not publish a stable anonymous ISO for this edition. Paste a URL the cluster can pull (Evaluation Center download, internal HTTP).';

export function templateParam(t: TemplateKind, name: string): string {
  const p = (t.parameters || []).find((x) => x.name === name);
  return (p?.value || '').trim();
}

/** os.template.kubevirt.io/win2k19 → win2k19 */
export function osLabelSku(t: TemplateKind): string {
  const labels = t.metadata.labels || {};
  const key = Object.keys(labels).find((k) => k.startsWith('os.template.kubevirt.io/win'));
  if (!key) return '';
  return key.slice('os.template.kubevirt.io/'.length);
}

export function stripVariantSuffixes(name: string): string {
  let n = name.toLowerCase();
  let changed = true;
  while (changed) {
    changed = false;
    for (const s of VARIANT_SUFFIXES) {
      const suf = `-${s}`;
      if (n.endsWith(suf)) {
        n = n.slice(0, -suf.length);
        changed = true;
      }
    }
  }
  return n;
}

/**
 * CNV virt Windows templates: name windows10-* / windows2k19-* / win11-* or
 * os.template.kubevirt.io/win* label. Not a hardcoded family allow-list.
 */
export function isWindowsVirtTemplate(t: TemplateKind): boolean {
  const name = (t.metadata.name || '').toLowerCase();
  if (/^windows(\d+|2k\d+)/.test(name) || /^win(10|11|2k)/.test(name)) {
    return true;
  }
  return Boolean(osLabelSku(t));
}

export function diskSizeFromTemplate(t: TemplateKind): string {
  const fromParam = templateParam(t, 'ROOT_DISK_SIZE');
  if (/^\d+[GM]i$/.test(fromParam)) return fromParam;
  const found = firstStorageRequest(t.objects);
  return /^\d+[GM]i$/.test(found) ? found : '';
}

function firstStorageRequest(value: unknown): string {
  if (!value || typeof value !== 'object') return '';
  if (Array.isArray(value)) {
    for (const item of value) {
      const hit = firstStorageRequest(item);
      if (hit) return hit;
    }
    return '';
  }
  const rec = value as Record<string, unknown>;
  const storage = rec.storage as Record<string, unknown> | undefined;
  const resources = storage?.resources as Record<string, unknown> | undefined;
  const requests = resources?.requests as Record<string, unknown> | undefined;
  if (typeof requests?.storage === 'string' && requests.storage) {
    return requests.storage;
  }
  for (const child of Object.values(rec)) {
    const hit = firstStorageRequest(child);
    if (hit) return hit;
  }
  return '';
}

export function displayNameFromTemplate(t: TemplateKind): string {
  const ann = t.metadata.annotations || {};
  const raw = (ann['openshift.io/display-name'] || '').trim();
  if (raw) return raw.replace(/\s+VM$/i, '').trim();
  const os = osLabelSku(t);
  return os ? `Windows (${os})` : t.metadata.name;
}

/** DATA_SOURCE_NAME from the template, else os label, else name prefix (windows10 → win10). */
export function familyIdFromTemplate(t: TemplateKind): string {
  const ds = templateParam(t, 'DATA_SOURCE_NAME');
  if (ds && isValidDiskName(ds)) return ds;
  const os = osLabelSku(t);
  if (os && isValidDiskName(os)) return os;
  const prefix = stripVariantSuffixes(t.metadata.name || '');
  const derived = prefix.replace(/^windows/, 'win');
  return isValidDiskName(derived) ? derived : '';
}

export function groupWindowsFamilies(templates: TemplateKind[]): WindowsFamily[] {
  const map = new Map<string, WindowsFamily>();
  for (const t of templates) {
    if (!t?.metadata?.name || !isWindowsVirtTemplate(t)) continue;
    const id = familyIdFromTemplate(t);
    if (!id) continue;
    const existing = map.get(id);
    if (!existing) {
      map.set(id, {
        id,
        prefix: stripVariantSuffixes(t.metadata.name),
        displayName: displayNameFromTemplate(t),
        templates: [t],
        goldenNamespace: templateParam(t, 'DATA_SOURCE_NAMESPACE') || GOLDEN_IMAGE_NAMESPACE,
        diskSize: diskSizeFromTemplate(t),
      });
      continue;
    }
    existing.templates.push(t);
    if (!existing.diskSize) {
      existing.diskSize = diskSizeFromTemplate(t);
    }
  }
  const list = Array.from(map.values());
  list.forEach((f) => {
    f.templates.sort((a, b) => {
      const an = `${a.metadata.namespace || ''}/${a.metadata.name}`;
      const bn = `${b.metadata.namespace || ''}/${b.metadata.name}`;
      return an.localeCompare(bn);
    });
  });
  list.sort((a, b) => a.id.localeCompare(b.id));
  return list;
}

export function templateRefOf(t: TemplateKind): string {
  return `${t.metadata.namespace || TEMPLATE_NAMESPACE}/${t.metadata.name}`;
}

/** Prefer *-server-medium or *-desktop-medium when present. */
export function preferredTemplateRef(templates: TemplateKind[]): string {
  const medium =
    templates.find((t) => /-(server|desktop)-medium$/.test(t.metadata.name)) ||
    templates.find((t) => /-medium$/.test(t.metadata.name)) ||
    templates[0];
  return medium ? templateRefOf(medium) : '';
}

export function normalizeSkuKey(id: string): string {
  return id.trim().toLowerCase().replace(/^windows/, 'win');
}

export function isClientSku(skuId: string): boolean {
  const n = normalizeSkuKey(skuId);
  return n === 'win10' || n === 'win11' || n.includes('win10') || n.includes('win11');
}

export function isoHintForSku(skuId: string): IsoHint {
  if (!skuId || skuId === 'custom') {
    return { helper: PASTE_HELPER };
  }
  const key = normalizeSkuKey(skuId);
  const known = ISO_HINTS[key];
  if (known?.url) {
    return { url: known.url, evalCenter: known.evalCenter, helper: SUGGEST_HELPER };
  }
  if (known?.evalCenter) {
    return { evalCenter: known.evalCenter, helper: PASTE_HELPER };
  }
  return { helper: PASTE_HELPER };
}

export function defaultDiskSizeForSku(skuId: string, family?: WindowsFamily): string {
  if (family?.diskSize) return family.diskSize;
  const n = normalizeSkuKey(skuId);
  if (n === 'win11' || n === 'win2k25') return '64Gi';
  return '60Gi';
}

export function installMemoryForSku(skuId: string): string {
  return normalizeSkuKey(skuId) === 'win11' ? '8Gi' : '4Gi';
}

export function installCoresForSku(skuId: string): number {
  return normalizeSkuKey(skuId) === 'win11' ? 2 : 2;
}

/** Short tile title. Known ids get friendly names; others use the template display name. */
export function editionTitle(id: string, displayName?: string): string {
  const n = normalizeSkuKey(id);
  switch (n) {
    case 'win10':
      return 'Windows 10';
    case 'win11':
      return 'Windows 11';
    case 'win2k16':
      return 'Windows Server 2016';
    case 'win2k19':
      return 'Windows Server 2019';
    case 'win2k22':
      return 'Windows Server 2022';
    case 'win2k25':
      return 'Windows Server 2025';
    default:
      break;
  }
  if (displayName) {
    return displayName.replace(/^Microsoft\s+/i, '').replace(/\s+VM$/i, '').trim() || id;
  }
  return id;
}

