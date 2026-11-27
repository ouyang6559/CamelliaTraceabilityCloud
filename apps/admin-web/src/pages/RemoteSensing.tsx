import { useMemo, useRef, useState } from 'react'
import {
  Button, Card, Form, Input, message, Modal, Select, Space, Table, Tabs, Tag, Upload, Typography,
} from 'antd'
import {
  CloudUploadOutlined, ReloadOutlined, GlobalOutlined, FileImageOutlined, 
} from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import maplibregl from 'maplibre-gl'
import { cogProtocol } from '@geomatico/maplibre-cog-protocol'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import MapView from '../components/MapView'
import { apiError, getJSON, listPage, postJSON, uploadFile } from '../api/client'

let cogRegistered = false
function ensureCogProtocol() {
  if (!cogRegistered) {
    maplibregl.addProtocol('cog', (params) => cogProtocol(params) as Promise<maplibregl.GetResourceResponse<ImageBitmap>>)
    cogRegistered = true
  }
}

const statusColor: Record<string, string> = {
  queued: 'default', running: 'orange', succeeded: 'green', failed: 'red', cancelled: 'default',
}

/** 遥感（Phase 4 基线，ADR-008）：影像处理任务（mock/NodeODM）+ STAC 资产目录。 */
export default function RemoteSensing() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [tab, setTab] = useState('jobs')
  const [modalOpen, setModalOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [uploaded, setUploaded] = useState<{ file_id: string; filename: string }[]>([])
  const [statusFilter, setStatusFilter] = useState('')
  const [collection, setCollection] = useState('orthophoto')
  const uploadRef = useRef<ReturnType<typeof Upload.Dragger>>(null)

  const jobs = useQuery({
    queryKey: ['rs-jobs', statusFilter],
    queryFn: () => {
      const params: Record<string, unknown> = {}
      if (statusFilter) params.status = statusFilter
      return listPage<Record<string, unknown>>('/remote-sensing/jobs', params)
    },
    refetchInterval: 3000,
  })

  const collections = useQuery({
    queryKey: ['stac-collections'],
    queryFn: () => getJSON<{ collections: { id: string; item_count: number }[] }>('/stac/collections'),
  })

  const items = useQuery({
    queryKey: ['stac-items', collection],
    queryFn: () => listPage<Record<string, unknown>>(`/stac/collections/${collection}/items`),
    enabled: tab === 'stac',
  })

  const mapRef = useRef<maplibregl.Map | null>(null)
  const [overlaidItem, setOverlaidItem] = useState<string | null>(null)

  // Footprint FeatureCollection from item.geometry (Polygon from bbox, Point).
  const footprintData = useMemo(() => {
    const rows = items.data?.items ?? []
    const features = rows
      .filter((r) => {
        const item = r.item as { geometry?: unknown } | null
        return item?.geometry != null
      })
      .map((r) => {
        const item = r.item as { geometry: GeoJSON.Geometry; id?: string }
        return { type: 'Feature' as const, id: String(r.id), geometry: item.geometry, properties: { id: String(r.id), title: (r.item as { properties?: { title?: string } })?.properties?.title ?? '' } }
      })
    return { type: 'FeatureCollection' as const, features }
  }, [items.data])

  function overlayAsset(row: Record<string, unknown>) {
    const item = row.item as { assets?: Record<string, { href: string }> } | null
    const asset = item?.assets ? Object.values(item.assets)[0] : null
    if (!asset?.href) return
    const map = mapRef.current
    if (!map) return
    ensureCogProtocol()
    const layerId = 'cog-overlay'
    const srcId = 'cog-src'
    if (map.getLayer(layerId)) map.removeLayer(layerId)
    if (map.getSource(srcId)) map.removeSource(srcId)
    const url = new URL(asset.href, window.location.origin).toString()
    // COG GeoTIFF renders via the cog:// protocol; plain images fall back to raster.
    const isTiff = url.toLowerCase().includes('.tif')
    map.addSource(srcId, isTiff
      ? { type: 'raster', url: 'cog://' + url }
      : { type: 'raster', tiles: [url], tileSize: 256 })
    map.addLayer({ id: layerId, type: 'raster', source: srcId, paint: { 'raster-opacity': 0.85 } })
    setOverlaidItem(String(row.id))
    message.success(t('rs.overlayOn'))
  }

  function clearOverlay() {
    const map = mapRef.current
    if (map?.getLayer('cog-overlay')) map.removeLayer('cog-overlay')
    if (map?.getSource('cog-src')) map.removeSource('cog-src')
    setOverlaidItem(null)
  }

  async function handleUpload(file: File) {
    try {
      const res = await uploadFile(file)
      setUploaded((u) => [...u, { file_id: res.id, filename: file.name }])
      message.success(`${file.name} ✓`)
    } catch (err) {
      message.error(apiError(err))
    }
    return false // prevent antd default upload
  }

  async function createJob(values: { job_type: string; mission_id?: string }) {
    if (uploaded.length === 0) {
      message.error(t('rs.needImages'))
      return
    }
    setCreating(true)
    try {
      await postJSON('/remote-sensing/jobs', {
        job_type: values.job_type,
        mission_id: values.mission_id || '',
        file_ids: uploaded.map((u) => u.file_id),
      })
      message.success(t('common.success'))
      setModalOpen(false)
      setUploaded([])
      void qc.invalidateQueries({ queryKey: ['rs-jobs'] })
    } catch (err) {
      message.error(apiError(err))
    } finally {
      setCreating(false)
    }
  }

  const jobColumns: ColumnsType<Record<string, unknown>> = [
    { key: 'job_type', title: '类型', dataIndex: 'job_type', width: 110,
      render: (v: string) => <Tag color="geekblue">{v}</Tag> },
    { key: 'status', title: t('common.status'), dataIndex: 'status', width: 110,
      render: (v: string) => <Tag color={statusColor[v] ?? 'default'}>{t(`status.${v}`) !== `status.${v}` ? t(`status.${v}`) : v}</Tag> },
    { key: 'progress', title: '进度', dataIndex: 'progress', width: 100,
      render: (v: unknown) => `${Number(v ?? 0)}%` },
    { key: 'processor', title: '处理器', dataIndex: 'processor', width: 100 },
    { key: 'mission_id', title: '任务', dataIndex: 'mission_id', width: 150,
      render: (v: string) => v ? <span className="mono">{v.slice(0, 12)}…</span> : '-' },
    { key: 'created_at', title: t('common.createdAt'), dataIndex: 'created_at', width: 170,
      render: (v: string) => v?.slice(0, 19).replace('T', ' ') },
    { key: 'error', title: '错误', dataIndex: 'error',
      render: (v: string) => v ? <Typography.Text type="danger">{String(v).slice(0, 40)}</Typography.Text> : '-' },
  ]

  const itemColumns: ColumnsType<Record<string, unknown>> = [
    { key: 'id', title: 'STAC ID', dataIndex: 'id',
      render: (v: string) => <span className="mono">{v.slice(0, 16)}…</span> },
    { key: 'collection_id', title: '集合', dataIndex: 'collection_id', width: 120 },
    { key: 'job_id', title: '任务', dataIndex: 'job_id', width: 150,
      render: (v: unknown) => (v ? <span className="mono">{String(v).slice(0, 12)}…</span> : '手动导入') },
    { key: 'created_at', title: t('common.createdAt'), dataIndex: 'created_at', width: 170,
      render: (v: string) => v?.slice(0, 19).replace('T', ' ') },
    { key: 'actions', title: t('common.actions'), width: 200,
      render: (_, row) => (
        <Space size="small">
          <a onClick={() => {
            const item = row.item as { assets?: Record<string, { href: string }> } | null
            const asset = item?.assets ? Object.values(item.assets)[0] : null
            if (asset?.href) window.open(asset.href, '_blank')
          }}>
            <FileImageOutlined /> {t('rs.openAsset')}
          </a>
          <a onClick={() => overlayAsset(row)}>
            {t('rs.overlay')}
          </a>
        </Space>
      ) },
  ]

  return (
    <Card className="page-card">
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          {
            key: 'jobs',
            label: t('rs.jobsTab'),
            children: (
              <>
                <div className="toolbar">
                  <Select
                    allowClear placeholder={t('common.status')} style={{ width: 140 }}
                    options={['queued', 'running', 'succeeded', 'failed', 'cancelled'].map((v) => ({ value: v, label: v }))}
                    onChange={(v) => setStatusFilter(v ?? '')}
                  />
                  <Button icon={<ReloadOutlined />} onClick={() => void jobs.refetch()}>{t('common.refresh')}</Button>
                  <Button type="primary" icon={<CloudUploadOutlined />} onClick={() => setModalOpen(true)}>
                    {t('rs.newJob')}
                  </Button>
                </div>
                <Table
                  rowKey="id"
                  columns={jobColumns}
                  dataSource={jobs.data?.items ?? []}
                  loading={jobs.isLoading}
                  pagination={{ pageSize: 20, total: jobs.data?.total ?? 0 }}
                />
              </>
            ),
          },
          {
            key: 'stac',
            label: t('rs.stacTab'),
            children: (
              <>
                <div className="toolbar">
                  <Select
                    style={{ width: 180 }}
                    value={collection}
                    onChange={setCollection}
                    options={(collections.data?.collections ?? []).map((c) => ({
                      value: c.id, label: `${c.id} (${c.item_count})`,
                    }))}
                  />
                  <Button icon={<GlobalOutlined />} onClick={() => void collections.refetch()}>
                    {t('common.refresh')}
                  </Button>
                </div>
                <Table
                  rowKey="id"
                  columns={itemColumns}
                  dataSource={items.data?.items ?? []}
                  loading={items.isLoading}
                  pagination={{ pageSize: 20, total: items.data?.total ?? 0 }}
                />
              </>
            ),
          },
        ]}
      />

      <Modal
        open={modalOpen}
        title={t('rs.newJob')}
        onCancel={() => { setModalOpen(false); setUploaded([]) }}
        destroyOnClose
        footer={null}
      >
        <Upload.Dragger multiple beforeUpload={handleUpload} accept="image/*,.tif,.tiff">
          <p style={{ fontSize: 28, margin: '8px 0' }}>☁️</p>
          <Typography.Text>{t('rs.uploadHint')}</Typography.Text>
        </Upload.Dragger>
        <div style={{ margin: '12px 0' }}>
          {uploaded.map((u) => (
            <Tag key={u.file_id} closable onClose={() => setUploaded((list) => list.filter((x) => x.file_id !== u.file_id))}>
              {u.filename}
            </Tag>
          ))}
        </div>
        <Form layout="vertical" onFinish={createJob}>
          <Form.Item name="job_type" label={t('rs.jobType')} initialValue="orthophoto" rules={[{ required: true }]}>
            <Select options={[
              { value: 'orthophoto', label: '正射影像 Orthophoto' },
              { value: 'ndvi', label: 'NDVI 植被指数' },
              { value: 'dem', label: '高程 DEM' },
            ]} />
          </Form.Item>
          <Form.Item name="mission_id" label="关联任务 ID（可选）">
            <Input placeholder="missions/{id}" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={creating}>
            {t('rs.submitJob')}
          </Button>
        </Form>
      </Modal>
    </Card>
  )
}
