import { useState } from 'react'
import { Card, Input, Table, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { listPage } from '../api/client'

/** 审计日志：全部写操作自动落审计（specs/08.5），支持按资源/操作检索。 */
export default function AuditLogs() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [resourceType, setResourceType] = useState('')

  const query = useQuery({
    queryKey: ['audit-logs', page, search, resourceType],
    queryFn: () => {
      const params: Record<string, unknown> = { page, page_size: 30 }
      if (search) params.search = search
      if (resourceType) params.resource_type = resourceType
      return listPage<Record<string, unknown>>('/audit-logs', params)
    },
  })

  const columns: ColumnsType<Record<string, unknown>> = [
    { key: 'created_at', title: t('common.createdAt'), dataIndex: 'created_at', width: 170,
      render: (v: string) => v?.slice(0, 19).replace('T', ' ') },
    { key: 'actor_name', title: '操作人', dataIndex: 'actor_name', width: 120,
      render: (v: string) => v || '-' },
    { key: 'action', title: '动作', dataIndex: 'action', width: 90,
      render: (v: string) => <Tag color={v === 'delete' ? 'red' : v === 'create' ? 'green' : 'blue'}>{v}</Tag> },
    { key: 'resource_type', title: '资源', dataIndex: 'resource_type', width: 150 },
    { key: 'resource_id', title: '资源 ID', dataIndex: 'resource_id', width: 170,
      render: (v: string) => <span className="mono">{v?.slice(0, 16)}</span> },
    { key: 'ip', title: 'IP', dataIndex: 'ip', width: 130 },
    { key: 'new_value', title: 'Payload', dataIndex: 'new_value',
      render: (v: unknown) => {
        const s = JSON.stringify(v)
        return s && s !== '{}' ? <span className="mono">{s.slice(0, 90)}</span> : '-'
      } },
  ]

  return (
    <Card className="page-card">
      <div className="toolbar">
        <Input.Search placeholder="搜索动作" style={{ width: 200 }} onSearch={setSearch} allowClear />
        <Input placeholder="资源类型 (如 farms)" style={{ width: 200 }}
          onChange={(e) => setResourceType(e.target.value)} allowClear />
        <Typography.Text type="secondary">
          传输层自动记录所有写操作（不含密码/令牌），领域级 old/new 值由关键服务补充
        </Typography.Text>
      </div>
      <Table
        rowKey="id"
        columns={columns}
        dataSource={query.data?.items ?? []}
        loading={query.isLoading}
        pagination={{ current: page, pageSize: 30, total: query.data?.total ?? 0, onChange: (p) => setPage(p) }}
      />
    </Card>
  )
}
