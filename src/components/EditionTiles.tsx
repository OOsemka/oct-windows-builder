import { Card, CardBody, Gallery, GalleryItem, Label, Spinner } from '@patternfly/react-core';
import React, { FC } from 'react';
import { useTranslation } from 'react-i18next';

import { WindowsFamily, editionTitle } from '../utils/windows-skus';
import WindowsFlagMark from './WindowsFlagMark';

const I18N = 'plugin__oct-windows-builder';
const CUSTOM = 'custom';

type Props = {
  families: WindowsFamily[];
  sku: string;
  loading: boolean;
  built: Set<string>;
  onSelect: (sku: string) => void;
};

const EditionTiles: FC<Props> = ({ families, sku, loading, built, onSelect }) => {
  const { t } = useTranslation(I18N);

  if (loading && families.length === 0) {
    return <Spinner size="lg" aria-label={t('Loading')} />;
  }

  return (
    <Gallery className="wb-edition-gallery" hasGutter minWidths={{ default: '9.25rem' }} maxWidths={{ default: '12rem' }}>
      {families.map((f) => {
        const selected = sku === f.id;
        const title = editionTitle(f.id, f.displayName);
        const isBuilt = built.has(f.id);
        return (
          <GalleryItem key={f.id}>
            <Card
              className={`wb-edition-tile${selected ? ' wb-edition-tile-selected' : ''}${isBuilt ? ' wb-edition-tile-built' : ''}`}
              isCompact
            >
              <CardBody className="wb-edition-body">
                {isBuilt ? (
                  <Label className="wb-edition-built-badge" color="green" status="success" isCompact>
                    {t('Built')}
                  </Label>
                ) : null}
                <button
                  type="button"
                  className="wb-edition-hit"
                  aria-pressed={selected}
                  aria-label={isBuilt ? `${title}, ${t('Built')}` : title}
                  onClick={() => onSelect(f.id)}
                >
                  <WindowsFlagMark />
                  <span className="wb-edition-title">{title}</span>
                  <span className="wb-edition-sub">{f.id}</span>
                </button>
              </CardBody>
            </Card>
          </GalleryItem>
        );
      })}
      <GalleryItem>
        <Card className={`wb-edition-tile${sku === CUSTOM ? ' wb-edition-tile-selected' : ''}`} isCompact>
          <CardBody className="wb-edition-body">
            <button
              type="button"
              className="wb-edition-hit"
              aria-pressed={sku === CUSTOM}
              aria-label={t('Custom')}
              onClick={() => onSelect(CUSTOM)}
            >
              <WindowsFlagMark muted />
              <span className="wb-edition-title">{t('Custom')}</span>
              <span className="wb-edition-sub">{t('Your DataVolume name')}</span>
            </button>
          </CardBody>
        </Card>
      </GalleryItem>
    </Gallery>
  );
};

export default EditionTiles;
