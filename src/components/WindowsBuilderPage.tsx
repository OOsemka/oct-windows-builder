import {
  DocumentTitle,
  K8sResourceCommon,
  ListPageHeader,
  useK8sWatchResource,
} from '@openshift-console/dynamic-plugin-sdk';
import { useTranslation } from 'react-i18next';
import {
  ActionGroup,
  Alert,
  Breadcrumb,
  BreadcrumbItem,
  Button,
  Card,
  CardBody,
  CardTitle,
  Form,
  FormGroup,
  FormHelperText,
  FormSelect,
  FormSelectOption,
  HelperText,
  HelperTextItem,
  Label,
  PageSection,
  Radio,
  Spinner,
  Stack,
  StackItem,
  TextArea,
  TextInput,
} from '@patternfly/react-core';
import { Table, Thead, Tr, Th, Tbody, Td } from '@patternfly/react-table';
import React, { FC, useCallback, useEffect, useMemo, useState } from 'react';

import { recommendedAutounattend, WindowsSku } from '../utils/autounattend';
import { builderHealth, BuildRecord, listBuilds, startBuild } from '../utils/builder-api';
import {
  DataVolumeKind,
  DataVolumeModel,
  GOLDEN_IMAGE_NAMESPACE,
  PLUGIN_NAMESPACE,
  PRESET_DISKS,
  PresetDisk,
  StorageClassKind,
  StorageClassModel,
  TEMPLATE_NAMESPACE,
  TemplateKind,
  TemplateModel,
  dvStorageClass,
  getK8sErrorMessage,
  isDefaultStorageClass,
  isForbiddenError,
  isMissingCrdError,
  isValidDiskName,
  isWindowsTemplate,
  phaseToUi,
  virtioImageFromTemplate,
} from '../utils/k8s-resources';
import dashboardLogger from '../utils/logger';
import CommunityDisclaimer from './CommunityDisclaimer';
import './windows-builder.css';

const I18N = 'plugin__oct-windows-builder';
const LOG = 'WINDOWS_BUILDER';

const statusLabelColor = (status: string): 'green' | 'red' | 'orange' | 'blue' | 'grey' => {
  switch (status) {
    case 'Ready':
    case 'Succeeded':
      return 'green';
    case 'Error':
    case 'Failed':
      return 'red';
    case 'Installing':
    case 'Sysprep':
      return 'blue';
    case 'Pending':
      return 'orange';
    default:
      return 'grey';
  }
};

const WindowsBuilderPage: FC = () => {
  const { t } = useTranslation(I18N);

  const [dvGolden, dvGoldenLoaded, dvGoldenErr] = useK8sWatchResource<K8sResourceCommon[]>({
    groupVersionKind: {
      group: DataVolumeModel.apiGroup,
      version: DataVolumeModel.apiVersion,
      kind: DataVolumeModel.kind,
    },
    isList: true,
    namespaced: true,
    namespace: GOLDEN_IMAGE_NAMESPACE,
  });

  const [dvWork, dvWorkLoaded, dvWorkErr] = useK8sWatchResource<K8sResourceCommon[]>({
    groupVersionKind: {
      group: DataVolumeModel.apiGroup,
      version: DataVolumeModel.apiVersion,
      kind: DataVolumeModel.kind,
    },
    isList: true,
    namespaced: true,
    namespace: PLUGIN_NAMESPACE,
  });

  const [templates, templatesLoaded, templatesErr] = useK8sWatchResource<K8sResourceCommon[]>({
    groupVersionKind: {
      group: TemplateModel.apiGroup,
      version: TemplateModel.apiVersion,
      kind: TemplateModel.kind,
    },
    isList: true,
    namespaced: true,
    namespace: TEMPLATE_NAMESPACE,
  });

  const [storageClasses, scLoaded] = useK8sWatchResource<K8sResourceCommon[]>({
    groupVersionKind: {
      group: StorageClassModel.apiGroup,
      version: StorageClassModel.apiVersion,
      kind: StorageClassModel.kind,
    },
    isList: true,
    namespaced: false,
  });

  const [sku, setSku] = useState<WindowsSku>('win2k19');
  const [customDisk, setCustomDisk] = useState('');
  const [isoURL, setIsoURL] = useState('');
  const [storageClass, setStorageClass] = useState('');
  const [diskSize, setDiskSize] = useState('60Gi');
  const [xml, setXml] = useState(() => recommendedAutounattend('win2k19'));
  const [xmlTouched, setXmlTouched] = useState(false);
  const [templateMode, setTemplateMode] = useState<'existing' | 'custom'>('existing');
  const [templateRef, setTemplateRef] = useState('');
  const [customTemplate, setCustomTemplate] = useState('');
  const [virtioImage, setVirtioImage] = useState('');
  const [virtioPrefill, setVirtioPrefill] = useState(false);
  const [scPrefill, setScPrefill] = useState(false);
  const [builds, setBuilds] = useState<BuildRecord[]>([]);
  const [builderUp, setBuilderUp] = useState<boolean | null>(null);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<{ variant: 'success' | 'danger'; msg: string } | null>(null);

  const diskName = sku === 'custom' ? customDisk.trim() : sku;

  const windowsTemplates = useMemo(
    () => ((templates || []) as TemplateKind[]).filter((x) => x?.metadata?.name && isWindowsTemplate(x)),
    [templates],
  );

  const goldenDVs = useMemo(() => {
    const fromGolden = ((dvGolden || []) as DataVolumeKind[]).filter((d) => d?.metadata?.name);
    const fromWork = ((dvWork || []) as DataVolumeKind[]).filter((d) => d?.metadata?.name);
    const map = new Map<string, DataVolumeKind>();
    fromWork.forEach((d) => map.set(`${d.metadata.namespace}/${d.metadata.name}`, d));
    fromGolden.forEach((d) => map.set(`${d.metadata.namespace}/${d.metadata.name}`, d));
    const list = Array.from(map.values());
    list.sort((a, b) => {
      const ap = PRESET_DISKS.includes(a.metadata.name as PresetDisk) ? 0 : 1;
      const bp = PRESET_DISKS.includes(b.metadata.name as PresetDisk) ? 0 : 1;
      if (ap !== bp) return ap - bp;
      return a.metadata.name.localeCompare(b.metadata.name);
    });
    return list;
  }, [dvGolden, dvWork]);

  const scList = useMemo(
    () => ((storageClasses || []) as StorageClassKind[]).filter((s) => s?.metadata?.name),
    [storageClasses],
  );

  useEffect(() => {
    if (xmlTouched) return;
    setXml(recommendedAutounattend(sku));
  }, [sku, xmlTouched]);

  useEffect(() => {
    if (virtioPrefill) return;
    for (const tpl of windowsTemplates) {
      const img = virtioImageFromTemplate(tpl);
      if (img) {
        setVirtioImage(img);
        setVirtioPrefill(true);
        return;
      }
    }
  }, [windowsTemplates, virtioPrefill]);

  useEffect(() => {
    if (scPrefill || !scLoaded) return;
    const fromDv = goldenDVs.map(dvStorageClass).find(Boolean);
    if (fromDv) {
      setStorageClass(fromDv);
      setScPrefill(true);
      return;
    }
    setScPrefill(true);
  }, [goldenDVs, scLoaded, scPrefill]);

  useEffect(() => {
    if (templateRef || windowsTemplates.length === 0) return;
    setTemplateRef(`${windowsTemplates[0].metadata.namespace || TEMPLATE_NAMESPACE}/${windowsTemplates[0].metadata.name}`);
  }, [windowsTemplates, templateRef]);

  const refreshBuilds = useCallback(async () => {
    try {
      const up = await builderHealth();
      setBuilderUp(up);
      if (!up) return;
      const next = await listBuilds();
      setBuilds(next);
    } catch (err) {
      setBuilderUp(false);
      dashboardLogger.warn(LOG, 'Builder poll failed', getK8sErrorMessage(err));
    }
  }, []);

  useEffect(() => {
    void refreshBuilds();
    const id = window.setInterval(() => void refreshBuilds(), 8000);
    return () => window.clearInterval(id);
  }, [refreshBuilds]);

  const onSku = (next: WindowsSku) => {
    setSku(next);
    setXmlTouched(false);
    setStatus(null);
  };

  const submit = useCallback(async () => {
    if (!isoURL.trim()) {
      setStatus({ variant: 'danger', msg: t('ISO URL is required.') });
      return;
    }
    if (!isValidDiskName(diskName)) {
      setStatus({ variant: 'danger', msg: t('Enter a valid DataVolume name.') });
      return;
    }
    setSaving(true);
    setStatus(null);
    try {
      let templateName = '';
      let templateNamespace = TEMPLATE_NAMESPACE;
      if (templateMode === 'existing' && templateRef) {
        const slash = templateRef.indexOf('/');
        templateNamespace = templateRef.slice(0, slash);
        templateName = templateRef.slice(slash + 1);
      } else {
        templateName = customTemplate.trim();
      }
      const rec = await startBuild({
        diskName,
        isoURL: isoURL.trim(),
        autounattend: xml,
        storageClassName: storageClass || undefined,
        diskSize,
        customTemplate: templateMode === 'custom',
        templateName: templateName || undefined,
        templateNamespace,
        virtioImage: virtioImage.trim() || undefined,
      });
      dashboardLogger.info(LOG, 'Started build', rec.diskName);
      setStatus({ variant: 'success', msg: t('Build started.') });
      await refreshBuilds();
    } catch (err) {
      dashboardLogger.error(LOG, 'Start build failed', getK8sErrorMessage(err));
      setStatus({ variant: 'danger', msg: `${t('Could not start the build.')} ${getK8sErrorMessage(err)}` });
    } finally {
      setSaving(false);
    }
  }, [
    isoURL,
    diskName,
    xml,
    storageClass,
    diskSize,
    templateMode,
    templateRef,
    customTemplate,
    virtioImage,
    t,
    refreshBuilds,
  ]);

  const dvErr = dvGoldenErr || dvWorkErr;
  const dvForbidden = isForbiddenError(dvErr);
  const dvMissing = isMissingCrdError(dvErr);
  const tplForbidden = isForbiddenError(templatesErr);
  const existing = goldenDVs.some((d) => d.metadata.name === diskName && d.metadata.namespace === GOLDEN_IMAGE_NAMESPACE);
  const buildByDisk = useMemo(() => {
    const m = new Map<string, BuildRecord>();
    builds.forEach((b) => m.set(b.diskName, b));
    return m;
  }, [builds]);

  const goComputeHub = () => {
    window.location.href = '/community-tools/compute';
  };

  return (
    <>
      <DocumentTitle>{t('Windows Builder')}</DocumentTitle>
      <PageSection type="breadcrumb">
        <Breadcrumb>
          <BreadcrumbItem
            component="a"
            onClick={(e) => {
              e.preventDefault();
              goComputeHub();
            }}
          >
            {t('Compute')}
          </BreadcrumbItem>
          <BreadcrumbItem isActive>{t('Windows Builder')}</BreadcrumbItem>
        </Breadcrumb>
      </PageSection>
      <ListPageHeader title={t('Windows Builder')} />
      <PageSection>
        <Stack hasGutter>
          <StackItem>
            <CommunityDisclaimer />
          </StackItem>
          <StackItem>
            <p className="wb-lead">
              {t('Build sysprepped Windows disks for OpenShift Virtualization. Provide an ISO URL the cluster can pull. No Tekton.')}
            </p>
          </StackItem>
          {dvMissing ? (
            <StackItem>
              <Alert variant="danger" isInline title={t('CDI or KubeVirt is not installed (DataVolume API missing). OpenShift Virtualization is required.')} />
            </StackItem>
          ) : null}
          {dvErr && !dvMissing ? (
            <StackItem>
              <Alert
                variant="danger"
                isInline
                title={dvForbidden || tplForbidden ? t('You do not have permission to list DataVolumes or Templates.') : t('Could not load DataVolumes.')}
              >
                {getK8sErrorMessage(dvErr)}
              </Alert>
            </StackItem>
          ) : null}
          {templatesErr && !isMissingCrdError(templatesErr) && !dvErr ? (
            <StackItem>
              <Alert variant="warning" isInline title={t('Could not load Templates.')}>
                {getK8sErrorMessage(templatesErr)}
              </Alert>
            </StackItem>
          ) : null}
          {builderUp === false ? (
            <StackItem>
              <Alert variant="warning" isInline title={t('Builder is not reachable. The windows-builder service must be running in oct-windows-builder.')} />
            </StackItem>
          ) : null}

          <StackItem>
            <Card>
              <CardTitle>{t('Golden images')}</CardTitle>
              <CardBody>
                {!dvGoldenLoaded && !dvWorkLoaded && !dvErr ? (
                  <Spinner size="lg" aria-label={t('Loading')} />
                ) : goldenDVs.length === 0 ? (
                  <p>{t('No Windows DataVolumes yet. Start a build to create win2k19, win2k25, win11, or a custom name.')}</p>
                ) : (
                  <Table variant="compact" aria-label={t('Golden images')}>
                    <Thead>
                      <Tr>
                        <Th>{t('Name')}</Th>
                        <Th>{t('Namespace')}</Th>
                        <Th>{t('Phase')}</Th>
                        <Th>{t('StorageClass')}</Th>
                        <Th>{t('Build')}</Th>
                      </Tr>
                    </Thead>
                    <Tbody>
                      {goldenDVs.map((dv) => {
                        const b = buildByDisk.get(dv.metadata.name);
                        const ui = b?.status || phaseToUi(dv.status?.phase);
                        return (
                          <Tr key={`${dv.metadata.namespace}/${dv.metadata.name}`}>
                            <Td dataLabel={t('Name')}>{dv.metadata.name}</Td>
                            <Td dataLabel={t('Namespace')}>{dv.metadata.namespace}</Td>
                            <Td dataLabel={t('Phase')}>
                              <Label color={statusLabelColor(ui)}>{dv.status?.phase || ui}</Label>
                            </Td>
                            <Td dataLabel={t('StorageClass')}>{dvStorageClass(dv) || t('default')}</Td>
                            <Td dataLabel={t('Build')}>
                              {b ? (
                                <span className="wb-status-row">
                                  <Label color={statusLabelColor(b.status)}>{t(b.status)}</Label>
                                  {b.message ? <span>{b.message}</span> : null}
                                </span>
                              ) : (
                                '—'
                              )}
                            </Td>
                          </Tr>
                        );
                      })}
                    </Tbody>
                  </Table>
                )}
                <FormHelperText>
                  <HelperText>
                    <HelperTextItem>
                      {t('Golden images are DataVolumes in openshift-virtualization-os-images plus any this plugin created.')}
                    </HelperTextItem>
                  </HelperText>
                </FormHelperText>
              </CardBody>
            </Card>
          </StackItem>

          <StackItem>
            <Card>
              <CardTitle>{t('Build')}</CardTitle>
              <CardBody>
                <Form
                  onSubmit={(e) => {
                    e.preventDefault();
                    void submit();
                  }}
                >
                  <FormGroup label={t('Windows edition')} fieldId="wb-sku" isRequired>
                    <FormSelect
                      id="wb-sku"
                      value={sku}
                      onChange={(_e, v) => onSku(v as WindowsSku)}
                      aria-label={t('Windows edition')}
                    >
                      <FormSelectOption value="win2k19" label={t('Windows Server 2019 (win2k19)')} />
                      <FormSelectOption value="win2k25" label={t('Windows Server 2025 (win2k25)')} />
                      <FormSelectOption value="win11" label={t('Windows 11 (win11)')} />
                      <FormSelectOption value="custom" label={t('Custom name')} />
                    </FormSelect>
                  </FormGroup>
                  {sku === 'custom' ? (
                    <FormGroup label={t('DataVolume name')} fieldId="wb-disk" isRequired>
                      <TextInput
                        id="wb-disk"
                        value={customDisk}
                        onChange={(_e, v) => setCustomDisk(v)}
                        validated={customDisk && !isValidDiskName(customDisk.trim()) ? 'error' : 'default'}
                        aria-label={t('DataVolume name')}
                      />
                      <FormHelperText>
                        <HelperText>
                          <HelperTextItem>{t('Must be a DNS-1123 name (lowercase, digits, dashes), max 50 characters.')}</HelperTextItem>
                        </HelperText>
                      </FormHelperText>
                    </FormGroup>
                  ) : null}
                  <FormGroup label={t('Windows ISO URL')} fieldId="wb-iso" isRequired>
                    <TextInput
                      id="wb-iso"
                      value={isoURL}
                      onChange={(_e, v) => setIsoURL(v)}
                      placeholder="https://"
                      aria-label={t('Windows ISO URL')}
                    />
                    <FormHelperText>
                      <HelperText>
                        <HelperTextItem>
                          {t('HTTP or HTTPS URL the cluster can pull. Do not use a lab-only host unless this cluster can reach it.')}
                        </HelperTextItem>
                      </HelperText>
                    </FormHelperText>
                  </FormGroup>
                  <FormGroup label={t('Template')} fieldId="wb-tpl-mode">
                    <Radio
                      id="wb-tpl-existing"
                      name="wb-tpl-mode"
                      label={t('Existing template')}
                      isChecked={templateMode === 'existing'}
                      onChange={() => setTemplateMode('existing')}
                    />
                    <Radio
                      id="wb-tpl-custom"
                      name="wb-tpl-mode"
                      label={t('Custom template')}
                      isChecked={templateMode === 'custom'}
                      onChange={() => setTemplateMode('custom')}
                    />
                  </FormGroup>
                  {templateMode === 'existing' ? (
                    <FormGroup label={t('Template')} fieldId="wb-tpl">
                      <FormSelect
                        id="wb-tpl"
                        value={templateRef}
                        onChange={(_e, v) => setTemplateRef(v)}
                        aria-label={t('Template')}
                        isDisabled={windowsTemplates.length === 0}
                      >
                        {windowsTemplates.length === 0 ? (
                          <FormSelectOption value="" label={t('No Windows Templates found. You can still type a custom template name.')} />
                        ) : (
                          windowsTemplates.map((tpl) => {
                            const val = `${tpl.metadata.namespace || TEMPLATE_NAMESPACE}/${tpl.metadata.name}`;
                            return <FormSelectOption key={val} value={val} label={val} />;
                          })
                        )}
                      </FormSelect>
                    </FormGroup>
                  ) : (
                    <FormGroup label={t('Custom template name')} fieldId="wb-tpl-name">
                      <TextInput
                        id="wb-tpl-name"
                        value={customTemplate}
                        onChange={(_e, v) => setCustomTemplate(v)}
                        aria-label={t('Custom template name')}
                      />
                      <FormHelperText>
                        <HelperText>
                          <HelperTextItem>{t('Created in the openshift namespace unless you select an existing Template.')}</HelperTextItem>
                        </HelperText>
                      </FormHelperText>
                    </FormGroup>
                  )}
                  <FormGroup label={t('Storage class')} fieldId="wb-sc">
                    <FormSelect
                      id="wb-sc"
                      value={storageClass}
                      onChange={(_e, v) => setStorageClass(v)}
                      aria-label={t('Storage class')}
                    >
                      <FormSelectOption value="" label={t('Cluster default')} />
                      {scList.map((sc) => (
                        <FormSelectOption
                          key={sc.metadata.name}
                          value={sc.metadata.name}
                          label={isDefaultStorageClass(sc) ? `${sc.metadata.name} (${t('default')})` : sc.metadata.name}
                        />
                      ))}
                    </FormSelect>
                    <FormHelperText>
                      <HelperText>
                        <HelperTextItem>{t('Omit to use the cluster default. Never hardcode a lab class.')}</HelperTextItem>
                      </HelperText>
                    </FormHelperText>
                  </FormGroup>
                  <FormGroup label={t('Disk size')} fieldId="wb-size">
                    <TextInput id="wb-size" value={diskSize} onChange={(_e, v) => setDiskSize(v)} aria-label={t('Disk size')} />
                  </FormGroup>
                  <FormGroup label={t('virtio-win containerDisk')} fieldId="wb-virtio">
                    <TextInput
                      id="wb-virtio"
                      value={virtioImage}
                      onChange={(_e, v) => setVirtioImage(v)}
                      aria-label={t('virtio-win containerDisk')}
                    />
                    <FormHelperText>
                      <HelperText>
                        <HelperTextItem>
                          {t('Optional. Pre-filled from an existing Windows Template when one lists a virtio-win image. The cluster must be able to pull it.')}
                        </HelperTextItem>
                      </HelperText>
                    </FormHelperText>
                  </FormGroup>
                  <FormGroup label={t('Autounattend.xml')} fieldId="wb-xml">
                    <Button
                      variant="link"
                      isInline
                      onClick={() => {
                        setXml(recommendedAutounattend(sku));
                        setXmlTouched(false);
                      }}
                    >
                      {t('Use recommended')}
                    </Button>
                    <TextArea
                      className="wb-xml"
                      id="wb-xml"
                      value={xml}
                      onChange={(_e, v) => {
                        setXml(v);
                        setXmlTouched(true);
                      }}
                      rows={14}
                      aria-label={t('Autounattend.xml')}
                    />
                    <FormHelperText>
                      <HelperText>
                        <HelperTextItem>
                          {t('Recommended XML loads virtio drivers from E:, partitions the disk (GPT/EFI), then sysprep /generalize /oobe /shutdown. Edit product key, image index, and the temporary AutoLogon password.')}
                        </HelperTextItem>
                      </HelperText>
                    </FormHelperText>
                  </FormGroup>
                  {existing ? (
                    <Alert
                      variant="warning"
                      isInline
                      title={t('This replaces DataVolume {{name}} if it already exists, after the new install disk is ready.', { name: diskName })}
                    />
                  ) : null}
                  {status ? <Alert variant={status.variant} isInline title={status.msg} /> : null}
                  <ActionGroup className="wb-actions">
                    <Button
                      variant="primary"
                      type="submit"
                      isDisabled={saving || !isoURL.trim() || !isValidDiskName(diskName)}
                      isLoading={saving}
                    >
                      {t('Start build')}
                    </Button>
                  </ActionGroup>
                </Form>
              </CardBody>
            </Card>
          </StackItem>
        </Stack>
      </PageSection>
    </>
  );
};

export default WindowsBuilderPage;
