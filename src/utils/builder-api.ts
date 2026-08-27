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

async function readError(res: Response): Promise<string> {
  const text = await res.text();
  try {
    const j = JSON.parse(text) as { error?: string; message?: string };
    return j.error || j.message || text || res.statusText;
  } catch {
    return text || res.statusText;
  }
}

export async function listBuilds(): Promise<BuildRecord[]> {
  const res = await fetch(`${BUILDER_PROXY}/api/v1/builds`);
  if (!res.ok) {
    throw new Error(await readError(res));
  }
  const data = (await res.json()) as { builds?: BuildRecord[] };
  return data.builds || [];
}

export async function startBuild(req: StartBuildRequest): Promise<BuildRecord> {
  const res = await fetch(`${BUILDER_PROXY}/api/v1/builds`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  if (!res.ok) {
    throw new Error(await readError(res));
  }
  return res.json() as Promise<BuildRecord>;
}

export async function builderHealth(): Promise<boolean> {
  try {
    const res = await fetch(`${BUILDER_PROXY}/healthz`);
    return res.ok;
  } catch {
    return false;
  }
}
