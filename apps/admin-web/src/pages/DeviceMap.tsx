import { useState } from 'react'
import { Card, Tag, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import MapView from '../components/MapView'
import { getJSON } from '../api/client'

interface FeatureProps { name: string; code: string; device_type: string; status: string }

/** 设备地图：在线/离线设备空间分布（specs/17.C 设备地图）。 */
export default function DeviceMap() {
  const { t } = useTranslation()
  const { data: geo } = useQuery({
    queryKey: ['devices-geojson'],
    queryFn: () => getJSON<GeoJSON.FeatureCollection>('/devices/geojson'),
    refetchInterval: 15_000,
  })
  const [selected, setSelected] = useState<FeatureProps | null>(null)

  return (
    <Card className="page-card" title={t('nav.deviceMap')}>
      <MapView
        data={geo}
        height={520}
        onMapReady={(map) => {
          map.on('click', 'ctc-features', (e) => {
            const props = e.features?.[0]?.properties as FeatureProps | undefined
            if (props) setSelected(props)
          })
        }}
      />
      <div style={{ marginTop: 12 }}>
        {selected ? (
          <Typography.Text>
            <Tag color={selected.status === 'online' ? 'green' : 'default'}>{selected.status}</Tag>
            <b>{selected.name}</b>（{selected.code}）· {selected.device_type}
          </Typography.Text>
        ) : (
          <Typography.Text type="secondary">点击地图上的设备查看状态 · Click a device on the map</Typography.Text>
        )}
      </div>
    </Card>
  )
}
