export const BUILDER_PROXY = '/api/proxy/plugin/oct-windows-builder/windows-builder';

export type BuildUiStatus = 'Pending' | 'Installing' | 'Sysprep' | 'Ready' | 'Error';

export type BuildRecord = {
  diskName: string;
  status: BuildUiStatus;
  message: string;
  isoHostPath?: string;
  templateName?: string;
  templateNamespace?: string;
  goldenNamespace?: string;
  virtioImage?: string;
  startedAt?: string;
  updatedAt?: string;
};

export type StartBuildRequest = {
  diskName: string;
  isoURL: string;
  autounattend: string;
  storageClassName?: string;
  diskSize?: string;
  isoSize?: string;
  goldenNamespace?: string;
  templateName?: string;
  templateNamespace?: string;
  customTemplate: boolean;
  virtioImage?: string;
  memory?: string;
  cores?: number;
};

function requireResponse(res: Response | undefined | null, path: string): Response {
  if (!res) {
    throw new Error(`Builder proxy returned no response (${path})`);
  }
  return res;
}

async function readError(res: Response): Promise<string> {
  let text = '';
  try {
    text = await res.text();
  } catch {
    return res.statusText || `HTTP ${res.status}`;
  }
  try {
    const j = JSON.parse(text) as { error?: string; message?: string };
    return j.error || j.message || text || res.statusText;
  } catch {
    return text || res.statusText || `HTTP ${res.status}`;
  }
}

async function readJson<T>(res: Response, fallback: T): Promise<T> {
  let text = '';
  try {
    text = await res.text();
  } catch {
    return fallback;
  }
  if (!text) return fallback;
  try {
    return JSON.parse(text) as T;
  } catch {
    throw new Error('Builder returned invalid JSON');
  }
}

export async function listBuilds(): Promise<BuildRecord[]> {
  const res = requireResponse(await fetch(`${BUILDER_PROXY}/api/v1/builds`), '/api/v1/builds');
  if (!res.ok) {
    throw new Error(await readError(res));
  }
  const data = await readJson<{ builds?: BuildRecord[] }>(res, { builds: [] });
  return data.builds || [];
}

export async function startBuild(req: StartBuildRequest): Promise<BuildRecord> {
  const res = requireResponse(
    await fetch(`${BUILDER_PROXY}/api/v1/builds`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    }),
    'POST /api/v1/builds',
  );
  if (!res.ok) {
    throw new Error(await readError(res));
  }
  return readJson<BuildRecord>(res, { diskName: req.diskName, status: 'Pending', message: '' });
}

export async function builderHealth(): Promise<boolean> {
  try {
    const res = await fetch(`${BUILDER_PROXY}/healthz`);
    return Boolean(res?.ok);
  } catch {
    return false;
  }
}
