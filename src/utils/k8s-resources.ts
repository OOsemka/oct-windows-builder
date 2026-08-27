import { K8sModel } from '@openshift-console/dynamic-plugin-sdk';

export const DataVolumeModel: K8sModel = {
  apiVersion: 'v1beta1',
  apiGroup: 'cdi.kubevirt.io',
  kind: 'DataVolume',
  abbr: 'DV',
  label: 'DataVolume',
  labelPlural: 'DataVolumes',
  plural: 'datavolumes',
  namespaced: true,
};

export const DataSourceModel: K8sModel = {
  apiVersion: 'v1beta1',
  apiGroup: 'cdi.kubevirt.io',
  kind: 'DataSource',
  abbr: 'DS',
  label: 'DataSource',
  labelPlural: 'DataSources',
  plural: 'datasources',
  namespaced: true,
};

export const VirtualMachineModel: K8sModel = {
  apiVersion: 'v1',
  apiGroup: 'kubevirt.io',
  kind: 'VirtualMachine',
  abbr: 'VM',
  label: 'VirtualMachine',
  labelPlural: 'VirtualMachines',
  plural: 'virtualmachines',
  namespaced: true,
};

export const TemplateModel: K8sModel = {
  apiVersion: 'v1',
  apiGroup: 'template.openshift.io',
  kind: 'Template',
  abbr: 'TPL',
  label: 'Template',
  labelPlural: 'Templates',
  plural: 'templates',
  namespaced: true,
};

export const StorageClassModel: K8sModel = {
  apiVersion: 'v1',
  apiGroup: 'storage.k8s.io',
  kind: 'StorageClass',
  abbr: 'SC',
  label: 'StorageClass',
  labelPlural: 'StorageClasses',
  plural: 'storageclasses',
  namespaced: false,
};

/** CNV golden-image namespace — product identity, not a lab name. */
export const GOLDEN_IMAGE_NAMESPACE = 'openshift-virtualization-os-images';

/** CNV Windows Templates typically live here. */
export const TEMPLATE_NAMESPACE = 'openshift';

export const PLUGIN_NAMESPACE = 'oct-windows-builder';

export const PRESET_DISKS = ['win2k19', 'win2k25', 'win11'] as const;
export type PresetDisk = typeof PRESET_DISKS[number];

export type DataVolumeKind = {
  apiVersion?: string;
  kind?: string;
  metadata: {
    name: string;
    namespace?: string;
    uid?: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
  };
  spec?: {
    storageClassName?: string;
    pvc?: {
      storageClassName?: string;
      resources?: { requests?: { storage?: string } };
    };
    storage?: {
      storageClassName?: string;
      resources?: { requests?: { storage?: string } };
    };
  };
  status?: {
    phase?: string;
    progress?: string;
    conditions?: Array<{ type?: string; status?: string; message?: string }>;
  };
};

export type StorageClassKind = {
  metadata: {
    name: string;
    annotations?: Record<string, string>;
  };
};

export type TemplateKind = {
  metadata: {
    name: string;
    namespace?: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
  };
  objects?: unknown[];
};

export function getK8sErrorMessage(err: unknown): string {
  if (!err) return '';
  if (typeof err === 'string') return err;
  const obj = err as {
    message?: string;
    json?: { message?: string };
  };
  return obj.json?.message || obj.message || String(err);
}

export function getK8sErrorCode(err: unknown): number | undefined {
  const obj = err as { json?: { code?: number }; status?: number; code?: number };
  return obj.json?.code ?? obj.status ?? obj.code;
}

export function isForbiddenError(err: unknown): boolean {
  const code = getK8sErrorCode(err);
  if (code === 403) return true;
  const msg = getK8sErrorMessage(err).toLowerCase();
  return msg.includes('forbidden') || msg.includes('cannot list resource');
}

export function isMissingCrdError(err: unknown): boolean {
  const code = getK8sErrorCode(err);
  if (code === 404) return true;
  const msg = getK8sErrorMessage(err).toLowerCase();
  return (
    msg.includes('could not find the requested resource') ||
    msg.includes('no matches for kind') ||
    msg.includes('the server could not find the requested resource')
  );
}

const DNS1123 = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

export function isValidDiskName(name: string): boolean {
  return name.length > 0 && name.length <= 50 && DNS1123.test(name);
}

export function dvStorageClass(dv: DataVolumeKind): string {
  return dv.spec?.storage?.storageClassName || dv.spec?.pvc?.storageClassName || '';
}

export function isDefaultStorageClass(sc: StorageClassKind): boolean {
  return sc.metadata.annotations?.['storageclass.kubernetes.io/is-default-class'] === 'true';
}

export function isWindowsTemplate(t: TemplateKind): boolean {
  const labels = t.metadata.labels || {};
  const ann = t.metadata.annotations || {};
  const blob = `${Object.keys(labels).join(' ')} ${Object.values(labels).join(' ')} ${Object.values(ann).join(' ')} ${t.metadata.name}`.toLowerCase();
  if (blob.includes('windows') || blob.includes('win2k') || blob.includes('win10') || blob.includes('win11')) {
    return true;
  }
  return Object.keys(labels).some((k) => k.startsWith('os.template.kubevirt.io/win'));
}

export function virtioImageFromTemplate(t: TemplateKind): string {
  const raw = JSON.stringify(t.objects || []);
  const virtioWin = raw.match(/"([^"]*virtio-win[^"]*)"/i);
  if (virtioWin) return virtioWin[1];
  const virtioDisk = raw.match(/"([^"]*virtio-container-disk[^"]*)"/i);
  return virtioDisk ? virtioDisk[1] : '';
}

export function phaseToUi(phase?: string): string {
  switch (phase) {
    case 'Succeeded':
      return 'Ready';
    case 'Failed':
      return 'Error';
    case 'Paused':
    case 'Pending':
    case 'WaitForFirstConsumer':
    case 'Unknown':
      return 'Pending';
    default:
      return phase || 'Pending';
  }
}
