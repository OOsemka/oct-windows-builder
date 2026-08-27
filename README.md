# Windows Builder (OpenShift Community Tools)

**Community project. Not officially supported by Red Hat.**

Standalone OpenShift Console plugin that builds **sysprepped** Windows disks for OpenShift Virtualization. The result is CDI **DataVolumes** named `win2k19`, `win2k25`, `win11` (or a custom DNS-1123 name) plus an optional OpenShift **Template**.

- **Plugin ID:** `oct-windows-builder`
- **Images:** `quay.io/<org>/oct-windows-builder:1.0.1-ocp4.22` / `:1.0.1-ocp4.21` and `quay.io/<org>/oct-windows-builder-builder:1.0.0-ocp4.22` / `:1.0.0-ocp4.21` (`<semver>-ocp<major.minor>`; same digest is OK)
- **Route:** `/community-tools/compute/windows-builder` (Community Tools → **Compute**)
- **Git branch:** `main` / optional `ocp-4.22` when PF/API differ

This is **not** the Community Tools catalog; hubs live in [oct-storefront](https://github.com/OOsemka/oct-storefront). Open from **Community Tools → Compute** after the storefront and this plugin are enabled.

Validated on OpenShift **4.22** (PatternFly 6). Requires **OpenShift Virtualization** (KubeVirt + CDI). No Tekton, no Argo CD, no extra operators.

## What it does

1. You paste a Windows ISO URL the cluster can pull (HTTP/HTTPS). Nothing is hardcoded.
2. You pick an edition preset (`win2k19` / `win2k25` / `win11`) or type a custom DataVolume name.
3. You choose an existing virt Windows **Template** (live list, typically in `openshift`) or type a custom template name.
4. Recommended **Autounattend.xml** is filled in (virtio driver paths, disk layout, **sysprep /generalize**). You can edit it.
5. **Start build** asks the in-cluster builder to: import the ISO → boot a VM → unattended install → sysprep shutdown → clone the disk to the golden DataVolume. Ready means CDI `status.phase=Succeeded`, never a fake status.

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

After **public** combined tags exist for **both** OpenShift minors (`:1.0.1-ocp4.22` and `:1.0.1-ocp4.21` for the plugin; builder may stay on `:1.0.0-ocp4.22` / `:1.0.0-ocp4.21` until it changes), open a PR against storefront `catalog/community.yaml` using [`catalog-tool.yaml`](catalog-tool.yaml) **including** `spec.versions[]`. Until then the Compute tile may exist without installable versions. Register `catalog/deploy/oct-windows-builder.yaml` in `BUNDLED_DEPLOY`. Always publish both minor tags. Never catalog a (version, OpenShift minor) row unless that exact tag is public.

## Deploy

`deploy/install.yaml` is for cluster-admin. Do not `oc apply` unless asked. Prefer storefront **Add** once the catalog tile **and** public combined tags exist.

## Still needed for a first real Windows guest

See [`docs/install-job.md`](docs/install-job.md). Typical gaps: a cluster-pullable **virtio-win** containerDisk (or floppy), guest tools MSI on that ISO, TPM/Secure Boot for Windows 11, and a multi-edition ISO `/IMAGE/INDEX` the Autounattend editor does not guess.
