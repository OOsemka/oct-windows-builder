import { normalizeSkuKey } from './windows-skus';

/**
 * Recommended Autounattend.xml for KubeVirt / OpenShift Virtualization golden images.
 *
 * Sources (not lab hosts):
 * - KMS GVLKs: https://learn.microsoft.com/en-us/windows-server/get-started/kms-client-activation-keys
 * - Answer files: https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/update-windows-settings-and-scripts-create-your-own-answer-file-sxs
 * - KubeVirt sysprep volume: https://kubevirt.io/user-guide/user_workloads/startup_scripts/
 * - virtio-win paths (viostor / NetKVM / Balloon, w10 w11 2k16–2k25):
 *   https://github.com/kubevirt/kubevirt-tekton-tasks (windows-efi-installer ConfigMaps)
 *   https://kubevirt.io/2021/Automated-Windows-Installation-With-Tekton-Pipelines.html
 * - Win11 LabConfig + BypassNRO: install VM uses UEFI with Secure Boot off so virtio can load;
 *   TPM is enabled on the VM spec. BypassNRO:
 *   https://github.com/kubevirt/kubevirt-tekton-tasks (windows11-autounattend)
 * - ImageInstall MetaData: /IMAGE/INDEX, /IMAGE/NAME, or /IMAGE/DESCRIPTION
 *   https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-setup-imageinstall-osimage-installfrom-metadata-key
 *
 * Evaluation Center install.wim **NAME** values are internal FLAGS strings
 * (`Windows Server 2019 SERVERDATACENTER`), not the Setup picker title.
 * `/IMAGE/DESCRIPTION` matches NAME on this media, not DISPLAYNAME. Feeding
 * DISPLAYNAME (or a retail title) as DESCRIPTION plus WillShowUI=OnError
 * yields an empty “No images are available” picker. Prefer `/IMAGE/INDEX`.
 *
 * Typical Microsoft Evaluation Center media (suggested ISO URLs in windows-skus.ts):
 * - Server SERVER_EVAL ISOs (2k16/2k19/2k22/2k25): four images — 1 Standard Core,
 *   2 Standard Desktop Experience, 3 Datacenter Core, 4 Datacenter Desktop Experience.
 *   Chris’s win2k19 ISO 17763.737…SERVER_EVAL_x64FRE (parsed from install.wim XML):
 *     1 NAME SERVERSTANDARDCORE     DISPLAYNAME Windows Server 2019 Standard Evaluation
 *     2 NAME SERVERSTANDARD         DISPLAYNAME Windows Server 2019 Standard Evaluation (Desktop Experience)
 *     3 NAME SERVERDATACENTERCORE   DISPLAYNAME Windows Server 2019 Datacenter Evaluation
 *     4 NAME SERVERDATACENTER       DISPLAYNAME Windows Server 2019 Datacenter Evaluation (Desktop Experience)
 *   DISPLAYNAME includes “Evaluation”; `/IMAGE/NAME` is the SERVER* string.
 *   KubeVirt tekton windows2k22 uses `/IMAGE/NAME` Windows Server 2022 SERVERDATACENTER
 *   (same FLAGS); INDEX 4 is Datacenter Desktop on SERVER_EVAL media.
 * - Client Enterprise Evaluation (Win10/11): usually a single image → INDEX 1.
 *
 * No Cloudbase-Init. FirstLogon ends with sysprep /generalize /oobe /shutdown.
 * Temporary AutoLogon password is a placeholder — never log it or product keys.
 */

/** Public Microsoft KMS client setup keys (GVLKs), not secrets or MAKs. */
const KMS_DATACENTER: Record<string, string> = {
  win2k16: 'CB7KF-BWN84-R7R2Y-793K2-8XDDG',
  win2k19: 'WMDGN-G9PQG-XVVXX-R3X43-63DFG',
  win2k22: 'WX4NM-KYWYW-QJJR4-XV3QB-6VM33',
  win2k25: 'D764K-2NDRG-47T6Q-P8T8W-YP6DF',
};

const KMS_ENTERPRISE = 'NPPR9-FWDCX-D2C8J-H872K-2YT43';
const KMS_PRO = 'W269N-WFGWX-YVC9B-4J6C9-T83GX';

type SkuKind = 'client10' | 'client11' | 'server' | 'generic';

export type AutounattendProfile = {
  skuId: string;
  virtioFolders: string[];
  kmsKey: string;
  computerName: string;
  /** /IMAGE/INDEX for typical Evaluation Center media (see file comment). */
  imageIndex: string;
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

function kmsKeyFor(skuId: string, kind: SkuKind): string {
  const n = normalizeSkuKey(skuId);
  if (kind === 'client10' || kind === 'client11') return KMS_ENTERPRISE;
  if (KMS_DATACENTER[n]) return KMS_DATACENTER[n];
  if (n.includes('2k25')) return KMS_DATACENTER.win2k25;
  if (n.includes('2k22')) return KMS_DATACENTER.win2k22;
  if (n.includes('2k19')) return KMS_DATACENTER.win2k19;
  if (n.includes('2k16')) return KMS_DATACENTER.win2k16;
  if (kind === 'server') return KMS_DATACENTER.win2k22;
  return KMS_PRO;
}

/**
 * /IMAGE/INDEX on typical Evaluation Center ISOs. Server 4-edition SERVER_EVAL
 * media uses 4 for Datacenter Desktop Experience; single-image client eval uses 1.
 */
function imageIndexFor(_skuId: string, kind: SkuKind): string {
  if (kind === 'server') return '4';
  return '1';
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
    kmsKey: kmsKeyFor(id, kind),
    computerName: computerNameFor(id),
    imageIndex: imageIndexFor(id, kind),
    kind,
  };
}

function driverPathsXml(folders: string[]): string {
  const kinds = ['viostor', 'NetKVM', 'Balloon'];
  let i = 1;
  const lines: string[] = [];
  for (const folder of folders) {
    for (const kind of kinds) {
      lines.push(`        <PathAndCredentials wcm:action="add" wcm:keyValue="${i}">
          <Path>E:\\${kind}\\${folder}\\amd64</Path>
        </PathAndCredentials>`);
      i += 1;
    }
  }
  return lines.join('\n');
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

function installFromXml(index: string): string {
  if (!index) return '';
  return `          <InstallFrom>
            <MetaData wcm:action="add">
              <Key>/IMAGE/INDEX</Key>
              <Value>${index}</Value>
            </MetaData>
          </InstallFrom>
`;
}

export function recommendedAutounattend(skuId: string): string {
  const p = profileForSku(skuId);
  const drivers = driverPathsXml(p.virtioFolders);
  const peExtra = p.kind === 'client11' ? win11PeCommands() : p.kind === 'client10' ? win10PeCommands() : '';
  const installFrom = installFromXml(p.imageIndex);
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
        <ProductKey>
          <Key>${p.kmsKey}</Key>
          <WillShowUI>OnError</WillShowUI>
        </ProductKey>
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
      <ProductKey>${p.kmsKey}</ProductKey>
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
        <SkipMachineOOBE>true</SkipMachineOOBE>
        <SkipUserOOBE>true</SkipUserOOBE>
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
        <SynchronousCommand wcm:action="add">
          <Order>1</Order>
          <Description>Install virtio guest tools if present on E:</Description>
          <CommandLine>cmd /c if exist E:\\virtio-win-gt-x64.msi msiexec /i E:\\virtio-win-gt-x64.msi /qn /norestart</CommandLine>
          <RequiresUserInput>false</RequiresUserInput>
        </SynchronousCommand>
        <SynchronousCommand wcm:action="add">
          <Order>2</Order>
          <Description>Install QEMU guest agent if present</Description>
          <CommandLine>cmd /c if exist E:\\guest-agent\\qemu-ga-x86_64.msi msiexec /i E:\\guest-agent\\qemu-ga-x86_64.msi /qn /norestart</CommandLine>
          <RequiresUserInput>false</RequiresUserInput>
        </SynchronousCommand>
        <SynchronousCommand wcm:action="add">
          <Order>3</Order>
          <Description>Disable AutoLogon</Description>
          <CommandLine>reg add "HKLM\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\Winlogon" /v AutoAdminLogon /t REG_SZ /d 0 /f</CommandLine>
          <RequiresUserInput>false</RequiresUserInput>
        </SynchronousCommand>
        <SynchronousCommand wcm:action="add">
          <Order>4</Order>
          <Description>Drop cached unattend so sysprep does not re-apply setup XML</Description>
          <CommandLine>cmd /c if exist C:\\Windows\\Panther\\unattend.xml move /Y C:\\Windows\\Panther\\unattend.xml C:\\Windows\\Panther\\unattend.install.xml</CommandLine>
          <RequiresUserInput>false</RequiresUserInput>
        </SynchronousCommand>
        <SynchronousCommand wcm:action="add">
          <Order>5</Order>
          <Description>Sysprep generalize and shutdown (golden image)</Description>
          <CommandLine>cmd /c C:\\Windows\\System32\\Sysprep\\sysprep.exe /generalize /oobe /shutdown /quiet</CommandLine>
          <RequiresUserInput>false</RequiresUserInput>
        </SynchronousCommand>
      </FirstLogonCommands>
    </component>
  </settings>
</unattend>
`;
}
