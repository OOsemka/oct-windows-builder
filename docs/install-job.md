# Windows golden-image install (no Tekton)

The builder Deployment in `oct-windows-builder` runs this sequence for each
**Start build**. It is a long-running goroutine, not a Tekton Pipeline and not
Argo CD. Status is stored on a ConfigMap (`wb-build-<disk>`) and never reports
**Ready** unless the **target** DataVolume has CDI `status.phase=Succeeded`.

## Namespaces

| Namespace | Role |
| --- | --- |
| `oct-windows-builder` | Workload: ISO DV, install DV, install VM, Autounattend ConfigMap |
| `openshift-virtualization-os-images` | Golden DataVolume + DataSource (`win2k19`, …). CNV product NS, not a lab name. |
| `openshift` | Default Template namespace (CNV Windows templates). |

The form can override golden-image namespace and StorageClass. Empty StorageClass = cluster default.

## Resources created

1. **ConfigMap** `wb-sysprep-<disk>`  
   Keys `autounattend.xml` and `Autounattend.xml` (same **install** XML). Mounted as a **SATA CD-ROM** from a ConfigMap volume (GitOps win2k19 layout), not a floppy and not `volumes[].sysprep`. Do **not** also publish `unattend.xml` — sysprep searches removable media for that name and would re-apply generalize+shutdown on clones. No Cloudbase-Init.

2. **DataVolume** `wb-iso-<disk>`  
   `spec.source.http.url` = user ISO URL. Size default 12Gi. Wait until `Succeeded`.

3. **DataVolume** `wb-install-<disk>`  
   `spec.source.blank`, size from the form (default 60Gi). Wait until `Succeeded` (blank bind).

4. **VirtualMachine** `wb-install-<disk>`  
   After the ISO DV succeeds, a Job patches El Torito in place (`efisys.bin` ← `efisys_noprompt.bin`) so UEFI does not wait for “Press any key to boot from CD or DVD”. The walker finds `efi/microsoft/boot` case-insensitively on Joliet, ISO9660, then UDF (Windows eval ISOs are UDF 1.02 with an ISO9660 stub). Then:  
   - Disk: blank DV, **SATA**, **bootOrder 1** (empty at first; firmware skips it; after Setup, Windows Boot Manager wins on reboot)  
   - CD-ROM **bootOrder 2**: patched ISO DV  
   - CD-ROM: cluster **virtio-win** containerDisk (`ConfigMap virtio-win` `data.virtio-win-image` in the OpenShift Virtualization namespace; never the HTTP download URL)  
   - Answer-file ConfigMap CD-ROM (no bootOrder; keys `autounattend.xml` / `Autounattend.xml` only)  
   - UEFI firmware (`secureBoot: false`); TPM enabled (needed for Windows 11)  
   - `runStrategy: RerunOnFailure`  
   - `evictionStrategy: None` (RWO install PVC cannot LiveMigrate)

5. **Wait** for the guest to power off after sysprep  
   Autounattend FirstLogonCommands install virtio-win-gt + qemu-ga from the virtio-win CD (letters D–G), delete cached Panther/Sysprep unattend files, then `sysprep.exe /generalize /oobe /shutdown /quiet /mode:vm`. A successful generalize ends in ACPI shutdown **after** qemu-guest-agent connected. A ~7 minute ACPI with no guest agent is **not** a sealed image (that is how 1.0.8 cloned an unsealed disk). Never-booted, empty picker, or shutdown before qemu-ga is **Error**.

6. **Delete** the VM (keep the install PVC).

7. **DataVolume** `<disk>` (e.g. `win2k19`) in the golden namespace  
   `spec.source.pvc` from the install PVC. Wait until `Succeeded`. If a DV with that name already exists, it is deleted **after** the install disk is ready, then recreated (short unavailability window; documented in the UI). CDI’s clone webhook requires `create` on `datavolumes/source` in the **source** (work) namespace; the install Role/ClusterRole grants that (parent `datavolumes` does not).

8. **DataSource** `<disk>` pointing at that PVC (so CNV Templates that `sourceRef` it keep working).

9. **Template** (optional)  
   Default is DataVolume only. If the user picks a flavor template, patch `DATA_SOURCE_NAME` / `DATA_SOURCE_NAMESPACE` on that Template. Custom names create a small VM Template.

## Autounattend

Recommended XML (editable in the form; **Use recommended for this version**) is generated per OS family. Sources:

- Working GitOps win2k19 (reference only, no Argo in this plugin): https://github.com/OOsemka/gitops-demo/tree/main/win2k19 — **no ProductKey**, ConfigMap CD `autounattend.xml`, `/IMAGE/INDEX` only
- Answer files: https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/update-windows-settings-and-scripts-create-your-own-answer-file-sxs
- KubeVirt ConfigMap CD: https://kubevirt.io/user-guide/user_workloads/startup_scripts/
- virtio-win paths (`E:\viostor\<sku>\amd64`, NetKVM, Balloon): https://github.com/kubevirt/kubevirt-tekton-tasks (windows-efi-installer ConfigMaps)

Per-SKU differences:

| Family | virtio folder | ProductKey | `/IMAGE/INDEX` (eval) | `/IMAGE/NAME` |
| --- | --- | --- | --- | --- |
| win10 | w10 | omit (eval) | 1 | omit |
| win11 | w11 | omit (eval) | 1 | omit (LabConfig TPM/Secure Boot bypass + BypassNRO) |
| win2k16 | 2k16 | omit (eval) | 2 | omit |
| win2k19 | 2k19 | omit (eval) | 2 | omit |
| win2k22 | 2k22 | omit (eval) | 2 | omit |
| win2k25 | 2k25 then 2k22 fallback | omit (eval) | 2 | omit |

A retail GVLK in UserData ProductKey makes Setup hide evaluation WIM images (“No images are available”). GitOps win2k19 and CNV windows2k22 omit the key. Retail volume media can add one from Microsoft’s KMS client list.

Recommended XML matches GitOps win2k19: INDEX **2** (Standard Desktop Experience) and **no** `/IMAGE/NAME` on SERVER_EVAL. INDEX 2 plus NAME `SERVERDATACENTER` conflict. Do not put DISPLAYNAME in `/IMAGE/DESCRIPTION`. Retail WIMs can edit the form.

Microsoft Evaluation Center **SERVER_EVAL** ISOs have four images: 1 Standard Core, 2 Standard Desktop, 3 Datacenter Core, 4 Datacenter Desktop Experience. Client eval is usually one image.

All SKUs: GPT/EFI, specialize PnP, FirstLogon virtio MSI + qemu-ga from D–G, delete cached unattend, **sysprep /generalize /oobe /shutdown /mode:vm**. No Cloudbase-Init. `WillShowUI OnError` if ImageInstall cannot match the WIM.

## ISO URL suggestions

Microsoft Evaluation Center is canonical. Direct HTTPS (en-US) is pre-filled only when a Microsoft CDN URL exists (HEAD-checked; paths can rotate). Windows 10/11 client eval has **no** stable anonymous ISO — paste a URL the cluster can pull.

- Server 2025: https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2025
- Server 2022: https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2022
- Server 2019: https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2019
- Server 2016: https://www.microsoft.com/en-us/evalcenter/evaluate-windows-server-2016
- Windows 11: https://www.microsoft.com/en-us/evalcenter/evaluate-windows-11-enterprise

Never invent a lab ISO.

## Status mapping

| UI | Meaning |
| --- | --- |
| Pending | Queued, ISO import, blank DV, VM create |
| Installing | VMI `Running` |
| Sysprep | VMI `Running` and guest agent connected (heuristic; sysprep still happens inside the guest) |
| Ready | Target DV `Succeeded` |
| Error | Any step failed (including missing CDI/KubeVirt, or guest shutdown before install finished) |

## TODO for a first real Windows install

These are **not** faked as Ready:

- **virtio-win containerDisk** — builder reads `ConfigMap/virtio-win` `data.virtio-win-image` (HCO). Do not use `virtio-win-image-download-url` (cluster route). The form pre-fills that image; empty means the same cluster default.  
- **Guest tools MSI** — `virtio-win-gt-x64.msi` and `guest-agent/qemu-ga-x86_64.msi` on that CD; Autounattend tries D: E: F: G:.  
- **Answer-file CD** — the ConfigMap is a SATA CD (GitOps win2k19), keys `autounattend.xml` and `Autounattend.xml` only (not `unattend.xml`). EFI noprompt keeps the blank disk at bootOrder 1.
- **Multi-edition ISO** — recommended XML uses `/IMAGE/INDEX` **2** (Standard Desktop) and omits `/IMAGE/NAME` on server eval media (GitOps win2k19). Retail or custom WIMs may need a different index (edit the form). Do not add a GVLK on SERVER_EVAL.
- **Windows 11** — needs TPM (enabled) and often Secure Boot; Secure Boot is off by default so unsigned test drivers can load. Turn it on in Autounattend/VM if your ISO requires it.
- **Replace golden DV** — deleting the old DV before the clone finishes would lose the previous image; the builder waits for the install disk first, then replaces. There is still a gap while the new clone runs.

Do not add Tekton, Argo, or extra operators to close these gaps.
