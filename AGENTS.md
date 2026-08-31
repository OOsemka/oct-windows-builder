# AGENTS.md — Windows Builder (OCT extension)

This is **OpenShift Community Tools (OCT)**, a **community project**, not an official Red Hat supported product. Do not describe it as official Red Hat software.

This repository is the **Windows Builder** ConsolePlugin. It is **not** the OCT storefront. Catalog hubs live in `oct-storefront`.

## Identifiers

| | Value |
| --- | --- |
| Plugin ID / ConsolePlugin / `package.json` `consolePlugin.name` | **`oct-windows-builder`** |
| Image | `quay.io/<org>/oct-windows-builder:1.0.3-ocp4.22` (and `:1.0.3-ocp4.21`; `<semver>-ocp<major.minor>`) |
| Builder image | `quay.io/<org>/oct-windows-builder-builder:1.0.3-ocp4.22` (and `:1.0.3-ocp4.21`) |
| i18n | `plugin__oct-windows-builder` |
| Route | `/community-tools/compute/windows-builder` |
| Proxy | `/api/proxy/plugin/oct-windows-builder/windows-builder` |
| CSS prefix | `wb-` |

Display name is **Windows Builder**. Hub is **Compute** (`category: compute`).

## What this plugin owns

- List CDI DataVolumes used as Windows golden images (template `DATA_SOURCE_NAME` such as `win2k19`, plus custom names)
- Start a long-running unattended Windows install (ISO URL + Autounattend + sysprep) via the in-cluster **builder**
- Optionally update an OpenShift Template `DATA_SOURCE_NAME` or create a custom Template

**No Tekton. No Argo CD.** Kubernetes Jobs are not required; the builder Deployment drives DataVolumes, a one-shot KubeVirt VM, then a clone DV.

## Architecture

| Piece | Role |
| --- | --- |
| Console plugin (`src/`) | Form + status. Kubernetes reads via Console SDK (user token). |
| Plugin nginx (`Containerfile`, `deploy/`) | Serves webpack `dist/`. ConsolePlugin **oct-windows-builder**. |
| Builder (`builder/`) | In-cluster Go service. Uses its ServiceAccount for DVs, VMs, PVCs, ConfigMaps, Templates, DataSources. |

Typical flow (see `docs/install-job.md`):

1. DataVolume for the Windows ISO (`source.http.url` the user supplied).
2. Blank DataVolume for the install disk.
3. ConfigMap `Autounattend.xml` / `unattend.xml` (KubeVirt **sysprep** volume).
4. VM boots ISO + virtio-win containerDisk (if the user provided an image) + Autounattend.
5. Unattended setup → FirstLogonCommands run **sysprep /generalize /oobe /shutdown**.
6. VMI phase `Succeeded` (guest powered off). Clone the disk to DataVolume `win2k19` (etc.) in `openshift-virtualization-os-images`.
7. Optional Template create/update; DataSource pointing at that PVC.

Do **not** mark a DataVolume Ready unless CDI `status.phase` is `Succeeded`.

## OpenShift and extension versions

Two axes in the catalog: git tag **`v1.x.x`** (semver) and optional branch **`ocp-X.Y`** when PatternFly or APIs diverge. Image tags **always** `<semver>-ocp<major.minor>` (e.g. `1.0.3-ocp4.22`).

- Git: `main` tracks the newest supported minor (currently **4.22**).
- PatternFly 6 on 4.22; do not mix PF majors on one branch.
- **Always publish both minors:** plugin and builder images get `<semver>-ocp4.21` and `<semver>-ocp4.22` (same digest if bits match). Catalog `versions[]` lists both when those tags exist.

**Do not list catalog `versions[].image` until that exact public combined tag exists.**

## Navigation

This plugin **does not** register the Community Tools section or hubs. Open from the storefront **Compute** tile or `/community-tools/compute/windows-builder`.

## No environment-specific hardcoding

Never bake in a lab StorageClass, ISO URL, VLAN, CIDR, hostname, or cluster domain. Omit PVC `storageClassName` (cluster default) or use a class the user picks from live StorageClasses. Optional: copy a class from an existing DV in `openshift-virtualization-os-images` — still no hardcoded class names. See `.cursor/rules/oct-no-env-hardcoding.mdc`.

Allowed product identity: plugin ID/namespace `oct-windows-builder`, CNV namespaces `openshift-virtualization-os-images` and `openshift` (Templates).

## Builder RBAC (not cluster-admin)

The builder ServiceAccount can get/list/watch/create/update/patch/delete DataVolumes, DataSources, VirtualMachines, VirtualMachineInstances, ConfigMaps, and PersistentVolumeClaims; get/list/watch/create/update/patch Templates; get/list StorageClasses; and in `oct-windows-builder` create Jobs/read pods (ISO El Torito patch). It cannot manage unrelated cluster-scoped resources. Document this in README. Do not log BMC passwords, ISO URL userinfo, Autounattend passwords, or kube tokens.

## Catalog tile

Storefront `catalog/community.yaml`: `metadata.name: oct-windows-builder`, `consolePlugin: oct-windows-builder`, `spec.href: /community-tools/compute/windows-builder`, `category: compute`, `spec.icon: tiles/oct-windows-builder.svg`. Copy `catalog-tool.yaml` into a storefront PR. **Never catalog a (version, OpenShift minor) row unless that exact combined tag is public.**

## Add must go Ready

Follow **oct-storefront** `docs/extension-standard.md`. Bundle must include plugin Deployment, builder Deployment, Services, RBAC, and ConsolePlugin proxy. Confirm **both** Deployments are Running before calling Add done.

## PatternFly 6

Import from `@patternfly/react-core` ^6. Do **not** import PatternFly CSS. Prefix new CSS `wb-`. Include `CommunityDisclaimer`.

## Verify

```bash
yarn install
yarn build
cd builder && go test ./... && go build -o windows-builder .
```

Do not `oc apply` or push images unless asked.
