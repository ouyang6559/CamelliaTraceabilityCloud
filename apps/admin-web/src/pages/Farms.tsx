import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import CrudPage from '../components/CrudPage'
import MapView from '../components/MapView'
import { getJSON } from '../api/client'

/** 基地管理：CRUD + 边界地图（GeoJSON 由后端 ST_AsGeoJSON 提供）。 */
export default function Farms() {
  const { t } = useTranslation()
  const { data: geo } = useQuery({
    queryKey: ['geojson-farms'],
    queryFn: () => getJSON<GeoJSON.FeatureCollection>('/geojson/farms'),
  })

  return (
    <CrudPage
      resourcePath="/farms"
      perm="farm"
      title={t('nav.farms')}
      columns={[
        { key: 'name', title: t('common.name') },
        { key: 'code', title: t('common.code') },
        { key: 'address', title: '地址 / Address' },
        { key: 'area_m2', title: '面积 m²', render: (row) => Number(row.area_m2 ?? 0).toLocaleString() },
        { key: 'created_at', title: t('common.createdAt') },
      ]}
      fields={[
        { name: 'name', label: t('common.name'), required: true },
        { name: 'code', label: t('common.code') },
        { name: 'address', label: '地址 / Address' },
        { name: 'area_m2', label: '面积 m²', type: 'number' },
        { name: 'description', label: t('common.description'), type: 'textarea' },
        { name: 'boundary', label: '边界 GeoJSON (Polygon, WGS84)', type: 'json' },
      ]}
      extra={<MapView data={geo} height={360} />}
      renderDetail={(row) => (
        <div>
          <p><b>{String(row.name)}</b>（{String(row.code ?? '-')}）</p>
          <p>{String(row.address ?? '')}</p>
        </div>
      )}
    />
  )
}
