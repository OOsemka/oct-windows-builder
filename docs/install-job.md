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
   Keys `Autounattend.xml` and `unattend.xml`. Mounted with KubeVirt `volumes[].sysprep.configMap` (floppy that Windows Setup reads).

2. **DataVolume** `wb-iso-<disk>`  
   `spec.source.http.url` = user ISO URL. Size default 12Gi. Wait until `Succeeded`.

3. **DataVolume** `wb-install-<disk>`  
   `spec.source.blank`, size from the form (default 60Gi). Wait until `Succeeded` (blank bind).

4. **VirtualMachine** `wb-install-<disk>`  
   After the ISO DV succeeds, a Job patches El Torito in place (`efisys.bin` ← `efisys_noprompt.bin`) so UEFI does not wait for “Press any key to boot from CD or DVD”. The walker finds `efi/microsoft/boot` case-insensitively on Joliet, ISO9660, then UDF (Windows eval ISOs are UDF 1.02 with an ISO9660 stub). Then:  
   - Disk: blank DV, **SATA**, **bootOrder 1** (empty at first; firmware skips it; after Setup, Windows Boot Manager wins on reboot)  
   - CD-ROM **bootOrder 2**: patched ISO DV  
   - Optional CD-ROM: virtio-win **containerDisk** (no bootOrder)  
   - Sysprep ConfigMap (no bootOrder)  
   - UEFI firmware (`secureBoot: false`); TPM enabled (needed for Windows 11)  
   - `runStrategy: RerunOnFailure`

5. **Wait** for VirtualMachineInstance `status.phase == Succeeded`  
   Autounattend FirstLogonCommands run `sysprep.exe /generalize /oobe /shutdown /quiet`. A successful generalize ends in ACPI shutdown → VMI Succeeded.

6. **Delete** the VM (keep the install PVC).

7. **DataVolume** `<disk>` (e.g. `win2k19`) in the golden namespace  
   `spec.source.pvc` from the install PVC. Wait until `Succeeded`. If a DV with that name already exists, it is deleted **after** the install disk is ready, then recreated (short unavailability window; documented in the UI).

8. **DataSource** `<disk>` pointing at that PVC (so CNV Templates that `sourceRef` it keep working).

9. **Template** (optional)  
   Default is DataVolume only. If the user picks a flavor template, patch `DATA_SOURCE_NAME` / `DATA_SOURCE_NAMESPACE` on that Template. Custom names create a small VM Template.

## Autounattend

Recommended XML (editable in the form; **Use recommended for this version**) is generated per OS family. Sources:

- Microsoft KMS GVLKs: https://learn.microsoft.com/en-us/windows-server/get-started/kms-client-activation-keys
- Answer files: https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/update-windows-settings-and-scripts-create-your-own-answer-file-sxs
- KubeVirt sysprep volume: https://kubevirt.io/user-guide/user_workloads/startup_scripts/
- virtio-win paths (`E:\viostor\<sku>\amd64`, NetKVM, Balloon): https://github.com/kubevirt/kubevirt-tekton-tasks (windows-efi-installer ConfigMaps) and https://kubevirt.io/2021/Automated-Windows-Installation-With-Tekton-Pipelines.html

Per-SKU differences:

| Family | virtio folder | Product key (GVLK) | Image description | Extra |
| --- | --- | --- | --- | --- |
| win10 | w10 | Enterprise | Windows 10 Enterprise | BypassNRO |
| win11 | w11 | Enterprise | Windows 11 Enterprise | LabConfig TPM/Secure Boot bypass + BypassNRO (install VM has TPM; Secure Boot off for virtio) |
| win2k16 | 2k16 | Datacenter | Windows Server 2016 Datacenter Evaluation | |
| win2k19 | 2k19 | Datacenter | …2019 Datacenter Evaluation (Desktop Experience) | |
| win2k22 | 2k22 | Datacenter | …2022 Datacenter Evaluation (Desktop Experience) | |
| win2k25 | 2k25 then 2k22 fallback | Datacenter | …2025 Datacenter Evaluation (Desktop Experience) | |

All SKUs: GPT/EFI, specialize PnP, FirstLogon virtio MSI + qemu-ga, drop cached unattend, **sysprep /generalize /oobe /shutdown**. No Cloudbase-Init. `WillShowUI OnError` if the eval image name does not match.

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
| Error | Any step failed (including missing CDI/KubeVirt) |

## TODO for a first real Windows install

These are **not** faked as Ready:

- **virtio-win containerDisk** — user must supply an image the cluster can pull (often `registry.redhat.io/container-native-virtualization/virtio-win`, which needs a pull secret). The form pre-fills from an existing Windows Template when one exists. Public `quay.io/kubevirt/virtio-container-disk` is not a full virtio-win ISO.
- **Guest tools MSI** — `virtio-win-gt-x64.msi` and `qemu-ga` live on that CD; Autounattend tries to install them if present.
- **Floppy vs CD** — KubeVirt `sysprep` volume is the floppy (`A:`). Some ISOs expect `Autounattend.xml` on a second CD instead; switch the VM spec if Setup ignores the floppy.
- **Multi-edition ISO** — set `/IMAGE/INDEX` (or `/IMAGE/NAME`) in Autounattend; the recommended XML does not guess an index.
- **Windows 11** — needs TPM (enabled) and often Secure Boot; Secure Boot is off by default so unsigned test drivers can load. Turn it on in Autounattend/VM if your ISO requires it.
- **Replace golden DV** — deleting the old DV before the clone finishes would lose the previous image; the builder waits for the install disk first, then replaces. There is still a gap while the new clone runs.

Do not add Tekton, Argo, or extra operators to close these gaps.
