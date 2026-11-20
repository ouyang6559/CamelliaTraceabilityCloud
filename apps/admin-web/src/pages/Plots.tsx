import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import CrudPage from '../components/CrudPage'
import MapView from '../components/MapView'
import { getJSON } from '../api/client'

/** 地块管理：CRUD + 地块边界地图。 */
export default function Plots() {
  const { t } = useTranslation()
  const { data: geo } = useQuery({
    queryKey: ['geojson-plots'],
    queryFn: () => getJSON<GeoJSON.FeatureCollection>('/geojson/plots'),
  })

  return (
    <CrudPage
      resourcePath="/plots"
      perm="plot"
      title={t('nav.plots')}
      filters={[{ name: 'farm_id', label: '基地 ID' }]}
      columns={[
        { key: 'name', title: t('common.name') },
        { key: 'code', title: t('common.code') },
        { key: 'farm_id', title: t('nav.farms') },
        { key: 'soil_type', title: '土壤' },
        { key: 'area_m2', title: '面积 m²', render: (row) => Number(row.area_m2 ?? 0).toLocaleString() },
        { key: 'source', title: '来源' },
        { key: 'status', title: t('common.status') },
      ]}
      fields={[
        { name: 'farm_id', label: '基地 ID (farms)', required: true },
        { name: 'name', label: t('common.name'), required: true },
        { name: 'code', label: t('common.code') },
        { name: 'boundary', label: '边界 GeoJSON (Polygon, WGS84)', type: 'json' },
        { name: 'area_m2', label: '面积 m²', type: 'number' },
        { name: 'soil_type', label: '土壤类型' },
        { name: 'source', label: '边界来源', type: 'select', initialValue: 'manual', options: [
          { value: 'manual', label: '手动勾绘' }, { value: 'survey', label: '测绘' }, { value: 'uav', label: '无人机' },
        ] },
        { name: 'accuracy_m', label: '精度 (m)', type: 'number' },
        { name: 'description', label: t('common.description'), type: 'textarea' },
      ]}
      extra={<MapView data={geo} />}
      renderDetail={(row) => (
        <div>
          <p><b>{String(row.name)}</b>（{String(row.code ?? '-')}）</p>
          <p>土壤: {String(row.soil_type ?? '-')} · 来源: {String(row.source ?? '-')}</p>
          {row.boundary ? (
            <pre className="mono" style={{ whiteSpace: 'pre-wrap' }}>
              {JSON.stringify(row.boundary, null, 2).slice(0, 800)}
            </pre>
          ) : null}
        </div>
      )}
    />
  )
}
