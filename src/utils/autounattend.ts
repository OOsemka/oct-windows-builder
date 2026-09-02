import { normalizeSkuKey } from './windows-skus';

/**
 * Recommended Autounattend.xml for KubeVirt / OpenShift Virtualization golden images.
 *
 * Working reference (no Argo CD / Tekton in this plugin):
 * https://github.com/OOsemka/gitops-demo/tree/main/win2k19
 * That tree’s Autounattend has **no ProductKey**, ImageInstall `/IMAGE/INDEX`
 * **2** (Standard Desktop Experience) only — no `/IMAGE/NAME`. The answer file
 * is a ConfigMap CD key `autounattend.xml` (not a KubeVirt sysprep volume).
 * INDEX 2 + NAME `SERVERDATACENTER` conflict; do not combine them.
 * GitOps put virtio-win on E: and scripts on F:; FirstLogon called
 * `f:\\post-install.ps1` (Cloudbase-Init + sysprep). This plugin does **not**
 * install Cloudbase-Init. Native `sysprep /generalize /oobe /shutdown` only.
 *
 * Other sources:
 * - Answer files: https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/update-windows-settings-and-scripts-create-your-own-answer-file-sxs
 * - KubeVirt sysprep / ConfigMap CD: https://kubevirt.io/user-guide/user_workloads/startup_scripts/
 * - virtio-win paths (viostor / NetKVM / Balloon, w10 w11 2k16–2k25):
 *   https://github.com/kubevirt/kubevirt-tekton-tasks (windows-efi-installer ConfigMaps)
 * - Win11 LabConfig + BypassNRO
 * - ImageInstall MetaData: /IMAGE/INDEX (GitOps); optional /IMAGE/NAME on retail WIMs only
 *   https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-setup-imageinstall-osimage-installfrom-metadata-key
 *
 * Evaluation Center install.wim **NAME** values are internal FLAGS strings
 * (`Windows Server 2019 SERVERDATACENTER`), not the Setup picker title.
 * `/IMAGE/DESCRIPTION` matches NAME on this media, not DISPLAYNAME.
 *
 * Typical Microsoft Evaluation Center media:
 * - Server SERVER_EVAL (2k16/2k19/2k22/2k25): four images — 1 Standard Core,
 *   2 Standard Desktop, 3 Datacenter Core, 4 Datacenter Desktop Experience.
 *   Chris’s win2k19 ISO (parsed from install.wim):
 *     1 NAME Windows Server 2019 SERVERSTANDARDCORE
 *     2 NAME Windows Server 2019 SERVERSTANDARD
 *     3 NAME Windows Server 2019 SERVERDATACENTERCORE
 *     4 NAME Windows Server 2019 SERVERDATACENTER
 * - Client Enterprise Evaluation: usually a single image → INDEX 1.
 *
 * A volume GVLK in UserData ProductKey makes Setup hide evaluation images
 * (“No images are available”, Next disabled) even when INDEX is correct.
 * Recommended XML omits ProductKey (GitOps win2k19 / CNV 2k22). Retail
 * volume media can add a key from:
 * https://learn.microsoft.com/en-us/windows-server/get-started/kms-client-activation-keys
 *
 * No Cloudbase-Init. FirstLogon installs virtio-win-gt + qemu-ga from the
 * standard CNV virtio-win CD (letters D–G; the Autounattend ConfigMap CD must
 * not steal E:), deletes cached Panther/Sysprep unattend files, then
 * sysprep /generalize /oobe /shutdown /mode:vm. Temporary AutoLogon password
 * is a placeholder — never log it or product keys.
 */

/** SATA CD letters on the install VM: Windows ISO, virtio-win, Autounattend CM. */
export const VIRTIO_CD_LETTERS = ['D', 'E', 'F', 'G'] as const;

type SkuKind = 'client10' | 'client11' | 'server' | 'generic';

export type AutounattendProfile = {
  skuId: string;
  virtioFolders: string[];
  computerName: string;
  /** /IMAGE/INDEX for typical Evaluation Center media. */
  imageIndex: string;
  /** /IMAGE/NAME (WIM NAME / FLAGS). Empty on recommended eval XML (INDEX only). */
  imageName: string;
  kind: SkuKind;
};

export function virtioFoldersForSku(skuId: string): string[] {
  const n = normalizeSkuKey(skuId);
  if (n === 'win11' || n.includes('win11')) return ['w11'];
  if (n === 'win10' || n.includes('win10')) return ['w10'];
  if (n.includes('2k25') || n.includes('2025')) return ['2k25', '2k22'];
  if (n.includes('2k22') || n.includes('2022')) return ['2k22'];
  if (n.includes('2k19') || n.includes('2019')) return ['2k19'];
  if (n.includes('2k16') || n.includes('2016')) return ['2k16'];
  if (n.includes('2k12')) return ['2k12R2'];
  return ['w10'];
}

function skuKind(skuId: string): SkuKind {
  const n = normalizeSkuKey(skuId);
  if (!n || n === 'custom') return 'generic';
  if (n === 'win11' || n.includes('win11')) return 'client11';
  if (n === 'win10' || n.includes('win10')) return 'client10';
  if (n.includes('2k') || n.includes('server') || n.includes('2016') || n.includes('2019') || n.includes('2022') || n.includes('2025')) {
    return 'server';
  }
  return 'generic';
}

/**
 * /IMAGE/INDEX on typical Evaluation Center ISOs. GitOps win2k19 uses **2**
 * (Standard Desktop Experience) on four-image SERVER_EVAL media. INDEX 4
 * (Datacenter Desktop) is not the proven selector for this plugin.
 */
function imageIndexFor(_skuId: string, kind: SkuKind): string {
  if (kind === 'server') return '2';
  return '1';
}

/** Recommended eval XML is INDEX only — do not emit /IMAGE/NAME. */
function imageNameFor(_skuId: string, _kind: SkuKind): string {
  return '';
}

function computerNameFor(skuId: string): string {
  const raw = skuId === 'custom' ? 'WindowsVM' : skuId;
  const s = raw.replace(/[^A-Za-z0-9-]/g, '').slice(0, 15);
  return s || 'WindowsVM';
}

export function profileForSku(skuId: string): AutounattendProfile {
  const id = skuId.trim() || 'custom';
  const kind = skuKind(id);
  return {
    skuId: id,
    virtioFolders: virtioFoldersForSku(id),
    computerName: computerNameFor(id),
    imageIndex: imageIndexFor(id, kind),
    imageName: imageNameFor(id, kind),
    kind,
  };
}

function driverPathsXml(folders: string[]): string {
  const kinds = ['viostor', 'NetKVM', 'Balloon'];
  let i = 1;
  const lines: string[] = [];
  // Install VM CDs: Windows ISO, virtio-win, Autounattend ConfigMap. Letter
  // assignment varies; list D–G so WinPE finds viostor/NetKVM/Balloon on the
  // virtio-win containerDisk instead of the answer-file CD (that used to be E:).
  for (const letter of VIRTIO_CD_LETTERS) {
    for (const folder of folders) {
      for (const kind of kinds) {
        lines.push(`        <PathAndCredentials wcm:action="add" wcm:keyValue="${i}">
          <Path>${letter}:\\${kind}\\${folder}\\amd64</Path>
        </PathAndCredentials>`);
        i += 1;
      }
    }
  }
  return lines.join('\n');
}

function firstLogonCommandsXml(): string {
  let order = 1;
  const cmds: string[] = [];
  const add = (desc: string, cmd: string) => {
    cmds.push(`        <SynchronousCommand wcm:action="add">
          <Order>${order}</Order>
          <Description>${desc}</Description>
          <CommandLine>${cmd}</CommandLine>
          <RequiresUserInput>false</RequiresUserInput>
        </SynchronousCommand>`);
    order += 1;
  };
  for (const d of VIRTIO_CD_LETTERS) {
    add(
      `Install virtio-win guest tools from ${d}: if present`,
      `cmd /c if exist ${d}:\\virtio-win-gt-x64.msi msiexec /i ${d}:\\virtio-win-gt-x64.msi /qn /norestart`,
    );
  }
  for (const d of VIRTIO_CD_LETTERS) {
    add(
      `Install QEMU guest agent from ${d}: if present`,
      `cmd /c if exist ${d}:\\guest-agent\\qemu-ga-x86_64.msi msiexec /i ${d}:\\guest-agent\\qemu-ga-x86_64.msi /qn /norestart`,
    );
  }
  add(
    'Disable AutoLogon',
    'reg add "HKLM\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\Winlogon" /v AutoAdminLogon /t REG_SZ /d 0 /f',
  );
  add('Wait so qemu-guest-agent can register before sysprep', 'cmd /c timeout /t 25 /nobreak');
  add(
    'Remove Panther unattend.xml',
    'cmd /c if exist C:\\Windows\\Panther\\unattend.xml del /f /q C:\\Windows\\Panther\\unattend.xml',
  );
  add(
    'Remove Panther Autounattend.xml',
    'cmd /c if exist C:\\Windows\\Panther\\Autounattend.xml del /f /q C:\\Windows\\Panther\\Autounattend.xml',
  );
  add(
    'Remove Sysprep unattend.xml',
    'cmd /c if exist C:\\Windows\\System32\\Sysprep\\unattend.xml del /f /q C:\\Windows\\System32\\Sysprep\\unattend.xml',
  );
  add(
    'Remove Panther Unattend directory',
    'cmd /c if exist C:\\Windows\\Panther\\Unattend rd /s /q C:\\Windows\\Panther\\Unattend',
  );
  add(
    'Remove Sysprep Panther directory',
    'cmd /c if exist C:\\Windows\\System32\\Sysprep\\Panther rd /s /q C:\\Windows\\System32\\Sysprep\\Panther',
  );
  add(
    'Sysprep generalize and shutdown (golden image; no Cloudbase-Init)',
    'cmd /c C:\\Windows\\System32\\Sysprep\\sysprep.exe /generalize /oobe /shutdown /quiet /mode:vm',
  );
  return cmds.join('\n');
}

function win11PeCommands(): string {
  return `      <RunSynchronous>
        <RunSynchronousCommand wcm:action="add">
          <Order>1</Order>
          <Description>Bypass TPM check (VM has TPM; required if firmware check fails)</Description>
          <Path>cmd /c reg add HKLM\\SYSTEM\\Setup\\LabConfig /v BypassTPMCheck /t REG_DWORD /d 1 /f</Path>
        </RunSynchronousCommand>
        <RunSynchronousCommand wcm:action="add">
          <Order>2</Order>
          <Description>Bypass Secure Boot check (install VM leaves Secure Boot off for virtio)</Description>
          <Path>cmd /c reg add HKLM\\SYSTEM\\Setup\\LabConfig /v BypassSecureBootCheck /t REG_DWORD /d 1 /f</Path>
        </RunSynchronousCommand>
        <RunSynchronousCommand wcm:action="add">
          <Order>3</Order>
          <Description>Bypass RAM check</Description>
          <Path>cmd /c reg add HKLM\\SYSTEM\\Setup\\LabConfig /v BypassRAMCheck /t REG_DWORD /d 1 /f</Path>
        </RunSynchronousCommand>
        <RunSynchronousCommand wcm:action="add">
          <Order>4</Order>
          <Description>Bypass Windows 11 network/MSA requirement (BypassNRO)</Description>
          <Path>cmd /c reg add HKLM\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\OOBE /v BypassNRO /t REG_DWORD /d 1 /f</Path>
        </RunSynchronousCommand>
      </RunSynchronous>
`;
}

function win10PeCommands(): string {
  return `      <RunSynchronous>
        <RunSynchronousCommand wcm:action="add">
          <Order>1</Order>
          <Description>Bypass network/MSA requirement (BypassNRO)</Description>
          <Path>cmd /c reg add HKLM\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\OOBE /v BypassNRO /t REG_DWORD /d 1 /f</Path>
        </RunSynchronousCommand>
      </RunSynchronous>
`;
}

function installFromXml(index: string, imageName: string): string {
  const metas: string[] = [];
  if (index) {
    metas.push(`            <MetaData wcm:action="add">
              <Key>/IMAGE/INDEX</Key>
              <Value>${index}</Value>
            </MetaData>`);
  }
  if (imageName) {
    metas.push(`            <MetaData wcm:action="add">
              <Key>/IMAGE/NAME</Key>
              <Value>${imageName}</Value>
            </MetaData>`);
  }
  if (!metas.length) return '';
  return `          <InstallFrom>
${metas.join('\n')}
          </InstallFrom>
`;
}

export function recommendedAutounattend(skuId: string): string {
  const p = profileForSku(skuId);
  const drivers = driverPathsXml(p.virtioFolders);
  const peExtra = p.kind === 'client11' ? win11PeCommands() : p.kind === 'client10' ? win10PeCommands() : '';
  const installFrom = installFromXml(p.imageIndex, p.imageName);
  const firstLogon = firstLogonCommandsXml();
  return `<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
  <settings pass="windowsPE">
    <component name="Microsoft-Windows-PnpCustomizationsWinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
      <DriverPaths>
${drivers}
      </DriverPaths>
    </component>
    <component name="Microsoft-Windows-International-Core-WinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <SetupUILanguage>
        <UILanguage>en-US</UILanguage>
      </SetupUILanguage>
      <InputLocale>0409:00000409</InputLocale>
      <SystemLocale>en-US</SystemLocale>
      <UILanguage>en-US</UILanguage>
      <UILanguageFallback>en-US</UILanguageFallback>
      <UserLocale>en-US</UserLocale>
    </component>
    <component name="Microsoft-Windows-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
${peExtra}      <DiskConfiguration>
        <Disk wcm:action="add">
          <DiskID>0</DiskID>
          <WillWipeDisk>true</WillWipeDisk>
          <CreatePartitions>
            <CreatePartition wcm:action="add">
              <Order>1</Order>
              <Type>EFI</Type>
              <Size>260</Size>
            </CreatePartition>
            <CreatePartition wcm:action="add">
              <Order>2</Order>
              <Type>MSR</Type>
              <Size>16</Size>
            </CreatePartition>
            <CreatePartition wcm:action="add">
              <Order>3</Order>
              <Type>Primary</Type>
              <Extend>true</Extend>
            </CreatePartition>
          </CreatePartitions>
          <ModifyPartitions>
            <ModifyPartition wcm:action="add">
              <Order>1</Order>
              <PartitionID>1</PartitionID>
              <Format>FAT32</Format>
              <Label>System</Label>
            </ModifyPartition>
            <ModifyPartition wcm:action="add">
              <Order>2</Order>
              <PartitionID>2</PartitionID>
            </ModifyPartition>
            <ModifyPartition wcm:action="add">
              <Order>3</Order>
              <PartitionID>3</PartitionID>
              <Format>NTFS</Format>
              <Label>OS</Label>
              <Letter>C</Letter>
            </ModifyPartition>
          </ModifyPartitions>
        </Disk>
      </DiskConfiguration>
      <ImageInstall>
        <OSImage>
${installFrom}          <InstallTo>
            <DiskID>0</DiskID>
            <PartitionID>3</PartitionID>
          </InstallTo>
          <InstallToAvailablePartition>false</InstallToAvailablePartition>
          <WillShowUI>OnError</WillShowUI>
        </OSImage>
      </ImageInstall>
      <UserData>
        <AcceptEula>true</AcceptEula>
        <FullName>Administrator</FullName>
        <Organization></Organization>
      </UserData>
    </component>
  </settings>
  <settings pass="offlineServicing">
    <component name="Microsoft-Windows-LUA-Settings" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <EnableLUA>false</EnableLUA>
    </component>
  </settings>
  <settings pass="generalize">
    <component name="Microsoft-Windows-Security-SPP" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <SkipRearm>1</SkipRearm>
    </component>
  </settings>
  <settings pass="specialize">
    <component name="Microsoft-Windows-PnpCustomizationsNonWinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
      <DriverPaths>
${drivers}
      </DriverPaths>
    </component>
    <component name="Microsoft-Windows-International-Core" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <InputLocale>0409:00000409</InputLocale>
      <SystemLocale>en-US</SystemLocale>
      <UILanguage>en-US</UILanguage>
      <UILanguageFallback>en-US</UILanguageFallback>
      <UserLocale>en-US</UserLocale>
    </component>
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <ComputerName>${p.computerName}</ComputerName>
    </component>
    <component name="Microsoft-Windows-Security-SPP-UX" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <SkipAutoActivation>true</SkipAutoActivation>
    </component>
  </settings>
  <settings pass="oobeSystem">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <AutoLogon>
        <Password>
          <Value>ChangeMe!</Value>
          <PlainText>true</PlainText>
        </Password>
        <Enabled>true</Enabled>
        <LogonCount>1</LogonCount>
        <Username>Administrator</Username>
      </AutoLogon>
      <OOBE>
        <HideEULAPage>true</HideEULAPage>
        <HideOEMRegistrationScreen>true</HideOEMRegistrationScreen>
        <HideOnlineAccountScreens>true</HideOnlineAccountScreens>
        <HideWirelessSetupInOOBE>true</HideWirelessSetupInOOBE>
        <NetworkLocation>Work</NetworkLocation>
        <ProtectYourPC>3</ProtectYourPC>
      </OOBE>
      <UserAccounts>
        <AdministratorPassword>
          <Value>ChangeMe!</Value>
          <PlainText>true</PlainText>
        </AdministratorPassword>
      </UserAccounts>
      <RegisteredOwner>Administrator</RegisteredOwner>
      <TimeZone>UTC</TimeZone>
      <FirstLogonCommands>
${firstLogon}
      </FirstLogonCommands>
    </component>
  </settings>
</unattend>
`;
}
