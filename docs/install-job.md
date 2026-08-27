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
   `spec.source.http.url` = user ISO URL. Size default 7Gi (ISO). Wait until `Succeeded`.

3. **DataVolume** `wb-install-<disk>`  
   `spec.source.blank`, size from the form (default 60Gi). Wait until `Succeeded` (blank bind).

4. **VirtualMachine** `wb-install-<disk>`  
   - CD-ROM bootOrder 1: ISO DV  
   - Disk: blank DV, **SATA** during install (Windows Setup sees the disk without viostor)  
   - Optional CD-ROM: virtio-win **containerDisk** (user-supplied image, or one discovered from an existing Windows Template)  
   - Sysprep ConfigMap  
   - UEFI firmware; TPM enabled (needed for Windows 11)  
   - `runStrategy: RerunOnFailure`

5. **Wait** for VirtualMachineInstance `status.phase == Succeeded`  
   Autounattend FirstLogonCommands run `sysprep.exe /generalize /oobe /shutdown /quiet`. A successful generalize ends in ACPI shutdown → VMI Succeeded.

6. **Delete** the VM (keep the install PVC).

7. **DataVolume** `<disk>` (e.g. `win2k19`) in the golden namespace  
   `spec.source.pvc` from the install PVC. Wait until `Succeeded`. If a DV with that name already exists, it is deleted **after** the install disk is ready, then recreated (short unavailability window; documented in the UI).

8. **DataSource** `<disk>` pointing at that PVC (so CNV Templates that `sourceRef` it keep working).

9. **Template**  
   Update the selected Template in place when it already targets this DV/DataSource name; otherwise create a small VM Template named by the user.

## Autounattend

Recommended XML (editable in the form) is derived from
[tekton-windows-pipeline](https://github.com/OOsemka/tekton-windows-pipeline)
and [gitops-demo/win2k19](https://github.com/OOsemka/gitops-demo/tree/main/win2k19):

- windowsPE PnP driver paths on the virtio CD (`E:\viostor\<sku>\amd64`, NetKVM, viorng)
- Wipe disk, GPT/EFI layout, install to C:
- Public Microsoft **KMS client setup keys** as placeholders (not a lab key; replace with a valid license)
- Temporary AutoLogon so FirstLogonCommands run
- Last command: **sysprep /generalize /oobe /shutdown /quiet**
- Optional: `msiexec` virtio guest tools / QEMU GA if those files exist on the virtio CD

Product keys in the sample XML are Microsoft’s published KMS client setup keys, not secrets.

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
