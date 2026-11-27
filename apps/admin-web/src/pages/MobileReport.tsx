import { useEffect, useState } from 'react'
import {
  Alert, Button, Card, Empty, Input, List, message, Modal, Select, Space, Tag, Typography,
} from 'antd'
import {
  CameraOutlined, EnvironmentOutlined, ReloadOutlined, WarningOutlined,
} from '@ant-design/icons'
import dayjs from 'dayjs'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, getJSON, listPage, postJSON, uploadFile } from '../api/client'

interface RecordRow {
  id: string
  name_zh: string
  category: string
  severity: string
  status: string
  discovered_at: string
  media: { file_id: string; filename?: string }[] | string
}

/**
 * 移动端病虫害/巡田上报（specs/11.3）：拍照（相机 capture）+ GPS + 字典选择，
 * 记录进入 disease_records 并可作为巡田证据。响应式窄屏布局。
 */
export default function MobileReport() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [photos, setPhotos] = useState<{ file_id: string; filename: string }[]>([])
  const [gps, setGps] = useState<{ lat: number; lng: number } | null>(null)
  const [form, setForm] = useState({ name_zh: '', category: 'disease', severity: 'medium' })
  const [note, setNote] = useState('')

  const records = useQuery({
    queryKey: ['mobile-records'],
    queryFn: () => listPage<RecordRow>('/disease-records', { page_size: 50 }),
    refetchInterval: 30_000,
  })

  const dictionary = useQuery({
    queryKey: ['disease-pests'],
    queryFn: () => getJSON<{ id: string; name_zh: string; category: string }[]>('/disease-pests'),
  })

  useEffect(() => {
    if ('geolocation' in navigator) {
      navigator.geolocation.getCurrentPosition(
        (pos) => setGps({ lat: pos.coords.latitude, lng: pos.coords.longitude }),
        () => undefined,
        { enableHighAccuracy: true, timeout: 8000 },
      )
    }
  }, [])

  function captureGPS() {
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        setGps({ lat: pos.coords.latitude, lng: pos.coords.longitude })
        message.success(t('mobile.gpsCaptured'))
      },
      (err) => message.error(`${t('mobile.gpsFailed')}: ${err.message}`),
      { enableHighAccuracy: true, timeout: 10000 },
    )
  }

  async function handlePhoto(file: File) {
    try {
      const res = await uploadFile(file)
      setPhotos((p) => [...p, { file_id: res.id, filename: file.name }])
      message.success(`${file.name} ✓`)
    } catch (err) {
      message.error(apiError(err))
    }
    return false
  }

  async function submit() {
    if (!form.name_zh) {
      message.error(t('mobile.needName'))
      return
    }
    setSubmitting(true)
    try {
      const body: Record<string, unknown> = {
        name_zh: form.name_zh,
        category: form.category,
        severity: form.severity,
        media: photos,
        status: 'open',
      }
      if (gps) {
        body.location = { type: 'Point', coordinates: [gps.lng, gps.lat] }
      }
      await postJSON('/disease-records', body)
      message.success(t('mobile.reported'))
      setOpen(false)
      setPhotos([])
      setNote('')
      void qc.invalidateQueries({ queryKey: ['mobile-records'] })
    } catch (err) {
      message.error(apiError(err))
    } finally {
      setSubmitting(false)
    }
  }

  function mediaTags(row: RecordRow): string[] {
    if (Array.isArray(row.media)) return row.media.map((m) => m.file_id)
    return []
  }

  return (
    <div style={{ padding: 12, maxWidth: 640, margin: '0 auto' }}>
      <Space style={{ marginBottom: 12, width: '100%', justifyContent: 'space-between' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          <WarningOutlined /> {t('mobile.reportTitle')}
        </Typography.Title>
        <Button icon={<ReloadOutlined />} onClick={() => void records.refetch()}>{t('common.refresh')}</Button>
      </Space>

      <Button
        type="primary" block size="large" icon={<CameraOutlined />}
        style={{ marginBottom: 14 }}
        onClick={() => setOpen(true)}
      >
        {t('mobile.newReport')}
      </Button>

      <List
        dataSource={records.data?.items ?? []}
        locale={{ emptyText: <Empty description={t('common.empty')} /> }}
        renderItem={(row) => (
          <Card size="small" style={{ marginBottom: 10 }}>
            <Space wrap style={{ marginBottom: 4 }}>
              <Tag color={row.category === 'disease' ? 'volcano' : row.category === 'pest' ? 'orange' : 'green'}>
                {row.category}
              </Tag>
              <b>{row.name_zh}</b>
              <Tag color={row.severity === 'high' ? 'red' : row.severity === 'medium' ? 'orange' : 'green'}>
                {row.severity}
              </Tag>
              <Tag>{row.status}</Tag>
            </Space>
            <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
              {dayjs(row.discovered_at).format('YYYY-MM-DD HH:mm')} · {mediaTags(row).length} {t('mobile.photos')}
            </Typography.Text>
          </Card>
        )}
      />

      <Modal
        open={open}
        title={t('mobile.newReport')}
        onCancel={() => setOpen(false)}
        onOk={submit}
        confirmLoading={submitting}
        okText={t('mobile.submitReport')}
        width="94%"
        style={{ top: 24 }}
      >
        <Space direction="vertical" style={{ width: '100%' }} size={10}>
          <Button icon={<EnvironmentOutlined />} onClick={captureGPS}>
            {t('mobile.captureGPS')}
          </Button>
          {gps && (
            <Typography.Text className="mono" style={{ fontSize: 12 }}>
              📍 {gps.lat.toFixed(5)}, {gps.lng.toFixed(5)}
            </Typography.Text>
          )}
          <Select
            style={{ width: '100%' }} size="large"
            value={form.category}
            onChange={(v) => setForm((f) => ({ ...f, category: v }))}
            options={[
              { value: 'disease', label: t('mobile.categoryDisease') },
              { value: 'pest', label: t('mobile.categoryPest') },
              { value: 'weed', label: t('mobile.categoryWeed') },
            ]}
          />
          <Select
            style={{ width: '100%' }} size="large" showSearch optionFilterProp="label"
            value={form.name_zh || undefined}
            placeholder={t('mobile.pickName')}
            onChange={(v) => setForm((f) => ({ ...f, name_zh: v }))}
            options={(dictionary.data ?? [])
              .filter((d) => d.category === form.category)
              .map((d) => ({ value: d.name_zh, label: d.name_zh }))}
          />
          <Select
            style={{ width: '100%' }} size="large"
            value={form.severity}
            onChange={(v) => setForm((f) => ({ ...f, severity: v }))}
            options={['low', 'medium', 'high'].map((v) => ({ value: v, label: v }))}
          />
          <label style={{ display: 'block' }}>
            <input
              type="file" accept="image/*" capture="environment" multiple
              style={{ display: 'none' }}
              onChange={(e) => {
                const files = Array.from(e.target.files ?? [])
                files.forEach(handlePhoto)
              }}
            />
            <Button block size="large" icon={<CameraOutlined />}>
              {t('mobile.takePhoto')}
            </Button>
          </label>
          {photos.length > 0 && (
            <Space wrap>
              {photos.map((p) => <Tag key={p.file_id} closable onClose={() => setPhotos((l) => l.filter((x) => x.file_id !== p.file_id))}>{p.filename}</Tag>)}
            </Space>
          )}
          <Input.TextArea rows={3} value={note} onChange={(e) => setNote(e.target.value)} placeholder={t('mobile.notePh')} />
        </Space>
      </Modal>
    </div>
  )
}
