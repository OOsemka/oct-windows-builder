# Windows Builder (OpenShift Community Tools)

**Community project. Not officially supported by Red Hat.**

Standalone OpenShift Console plugin that builds **sysprepped** Windows disks for OpenShift Virtualization. Golden **DataVolumes** use the name OpenShift templates expect (`DATA_SOURCE_NAME`: `win10`, `win11`, `win2k16`, `win2k19`, `win2k22`, `win2k25`, or a custom DNS-1123 name).

- **Plugin ID:** `oct-windows-builder`
- **Images:** `quay.io/<org>/oct-windows-builder:1.0.3-ocp4.22` / `:1.0.3-ocp4.21` and `quay.io/<org>/oct-windows-builder-builder:1.0.3-ocp4.22` / `:1.0.3-ocp4.21` (`<semver>-ocp<major.minor>`; same digest is OK)
- **Route:** `/community-tools/compute/windows-builder` (Community Tools → **Compute**)
- **Git branch:** `main` / optional `ocp-4.22` when PF/API differ

This is **not** the Community Tools catalog; hubs live in [oct-storefront](https://github.com/OOsemka/oct-storefront). Open from **Community Tools → Compute** after the storefront and this plugin are enabled.

Validated on OpenShift **4.22** (PatternFly 6). Requires **OpenShift Virtualization** (KubeVirt + CDI). No Tekton, no Argo CD, no extra operators.

## What it does

1. Live **Windows family tiles** (not a dropdown) from cluster Templates (`windows10-*`, `windows11-*`, `windows2k16-*`, …). Size/workload variants are grouped. Custom DataVolume name is extra.
2. Progressive form: edition → ISO (optional Microsoft eval URL) → Autounattend for that SKU → template (or DV only) → storage / Start build.
3. **Start build** asks the in-cluster builder to import the ISO, boot a VM, unattended install, sysprep shutdown, clone to the golden DataVolume. Ready means CDI `status.phase=Succeeded`.

## Contributing — cluster-portable code

Do **not** hardcode environment-specific values (StorageClass names, ISO URLs, VLANs, cluster domains). Storage: omit `storageClassName` or pick a live class in the form. Agents: `.cursor/rules/oct-no-env-hardcoding.mdc`.

## Builder permissions

The builder ServiceAccount is **not** cluster-admin. It can:

| API | Verbs |
| --- | --- |
| `datavolumes`, `datasources` (CDI) | get, list, watch, create, update, patch, delete |
| `virtualmachines`, `virtualmachineinstances` (KubeVirt) | get, list, watch, create, update, patch, delete |
| `templates` (`template.openshift.io`) | get, list, watch, create, update, patch |
| `configmaps`, `persistentvolumeclaims` | get, list, watch, create, update, patch, delete |
| `jobs` (`batch`, namespace Role) | get, list, watch, create, delete |
| `pods`, `pods/log` (namespace Role) | get, list (pods); get (logs) |
| `storageclasses` | get, list |

Template/DV **reads** in the UI use the signed-in user’s console token.

Do not log ISO credentials, Autounattend passwords, or API tokens. ISO URLs are logged as host/path only.

## Build

```bash
yarn install
yarn build
cd builder && go test ./... && go build -o windows-builder .
```

## Catalog

After **public** combined tags exist for **both** OpenShift minors (`:1.0.3-ocp4.22` and `:1.0.3-ocp4.21` for the plugin and builder), open a PR against storefront `catalog/community.yaml` using [`catalog-tool.yaml`](catalog-tool.yaml) **including** `spec.versions[]`. Until then the Compute tile may exist without installable versions. Register `catalog/deploy/oct-windows-builder.yaml` in `BUNDLED_DEPLOY`. Always publish both minor tags. Never catalog a (version, OpenShift minor) row unless that exact tag is public.

## Deploy

`deploy/install.yaml` is for cluster-admin. Do not `oc apply` unless asked. Prefer storefront **Add** once the catalog tile **and** public combined tags exist.

## Still needed for a first real Windows guest

See [`docs/install-job.md`](docs/install-job.md). Typical gaps: a cluster-pullable **virtio-win** containerDisk (or floppy), guest tools MSI on that ISO, TPM/Secure Boot for Windows 11, and a multi-edition ISO `/IMAGE/INDEX` the Autounattend editor does not guess.
