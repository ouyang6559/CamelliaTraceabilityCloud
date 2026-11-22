import { useState } from 'react'
import {
  Alert, Avatar, Button, Card, Checkbox, Form, Input, List, message, Modal,
  Popconfirm, Select, Space, Table, Tabs, Tag, Typography,
} from 'antd'
import { RobotOutlined, SendOutlined, UserOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, apiError, listPage, postJSON } from '../api/client'

interface ChatEntry {
  role: 'user' | 'ai'
  content: string
  type?: string
  confidence?: number
  sources?: { doc_id: string; title: string; category: string; score: number; excerpt: string }[]
}

/** AI 农业助手：知识库 RAG-lite（ADR-009）+ 对话输出标注为 AI 生成建议（specs/07.8）。 */
export default function AiAssistant() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [messages, setMessages] = useState<ChatEntry[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [useKnowledge, setUseKnowledge] = useState(true)
  const [kbModalOpen, setKbModalOpen] = useState(false)
  const [kbForm] = Form.useForm()

  const jobs = useQuery({
    queryKey: ['ai-jobs'],
    queryFn: () => listPage<Record<string, unknown>>('/ai/jobs', { page: 1, page_size: 10 }),
    refetchInterval: 20_000,
  })

  const docs = useQuery({
    queryKey: ['ai-knowledge'],
    queryFn: () => listPage<Record<string, unknown>>('/ai/knowledge'),
  })

  async function send() {
    const text = input.trim()
    if (!text) return
    setInput('')
    setMessages((m) => [...m, { role: 'user', content: text }])
    setLoading(true)
    try {
      const res = await postJSON<{
        output: string; output_type: string; confidence: number
        sources?: ChatEntry['sources']
      }>('/ai/chat', {
        messages: [{ role: 'user', content: text }],
        use_knowledge: useKnowledge,
      })
      setMessages((m) => [...m, {
        role: 'ai', content: res.output,
        type: res.output_type, confidence: res.confidence, sources: res.sources,
      }])
      void qc.invalidateQueries({ queryKey: ['ai-jobs'] })
    } catch (err) {
      message.error(apiError(err))
    } finally {
      setLoading(false)
    }
  }

  async function review(jobId: string, action: 'approved' | 'rejected') {
    try {
      await postJSON(`/ai/jobs/${jobId}/review`, { action })
      message.success(t('common.success'))
      void qc.invalidateQueries({ queryKey: ['ai-jobs'] })
    } catch (err) {
      message.error(apiError(err))
    }
  }

  async function createDoc(values: { title: string; category: string; content: string }) {
    try {
      await postJSON('/ai/knowledge', values)
      message.success(t('common.success'))
      setKbModalOpen(false)
      kbForm.resetFields()
      void qc.invalidateQueries({ queryKey: ['ai-knowledge'] })
    } catch (err) {
      message.error(apiError(err))
    }
  }

  async function deleteDoc(id: string) {
    try {
      await api.delete(`/ai/knowledge/${id}`)
      void qc.invalidateQueries({ queryKey: ['ai-knowledge'] })
    } catch (err) {
      message.error(apiError(err))
    }
  }

  const docColumns: ColumnsType<Record<string, unknown>> = [
    { key: 'title', title: t('common.name'), dataIndex: 'title' },
    { key: 'category', title: '分类', dataIndex: 'category', width: 110,
      render: (v: string) => <Tag>{v}</Tag> },
    { key: 'chunk_count', title: t('rs.chunks'), dataIndex: 'chunk_count', width: 90 },
    { key: 'source', title: '来源', dataIndex: 'source', width: 90 },
    { key: 'created_at', title: t('common.createdAt'), dataIndex: 'created_at', width: 170,
      render: (v: string) => v?.slice(0, 19).replace('T', ' ') },
    { key: 'actions', title: t('common.actions'), width: 90,
      render: (_, row) => (
        <Popconfirm title={t('common.confirmDelete')} onConfirm={() => deleteDoc(String(row.id))}>
          <Button size="small" danger>{t('common.delete')}</Button>
        </Popconfirm>
      ) },
  ]

  return (
    <div style={{ padding: 16 }}>
      <Alert type="info" showIcon message={t('ai.disclaimer')} style={{ marginBottom: 16 }} />
      <Card>
        <Tabs
          defaultActiveKey="chat"
          items={[
            {
              key: 'chat',
              label: t('ai.chat'),
              children: (
                <>
                  <div style={{ minHeight: 320, maxHeight: 480, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 12 }}>
                    {messages.length === 0 && (
                      <Typography.Text type="secondary">{t('ai.placeholder')}…</Typography.Text>
                    )}
                    {messages.map((m, i) => (
                      <div key={i} style={{ display: 'flex', gap: 8, flexDirection: m.role === 'user' ? 'row-reverse' : 'row' }}>
                        <Avatar icon={m.role === 'user' ? <UserOutlined /> : <RobotOutlined />} style={m.role === 'ai' ? { backgroundColor: '#2e7d32' } : undefined} />
                        <div style={{
                          maxWidth: '75%', padding: '8px 12px', borderRadius: 10, whiteSpace: 'pre-wrap',
                          background: m.role === 'user' ? '#e8f5e9' : '#f5f5f5',
                        }}>
                          {m.content}
                          {m.type && (
                            <div style={{ marginTop: 6 }}>
                              <Tag color="green">{m.type}</Tag>
                              {m.confidence != null && <Tag>confidence {m.confidence.toFixed(2)}</Tag>}
                            </div>
                          )}
                          {m.sources && m.sources.length > 0 && (
                            <div style={{ marginTop: 6, fontSize: 12, color: '#666' }}>
                              {t('ai.sources')}:
                              {m.sources.map((src) => (
                                <Tag key={src.doc_id} style={{ fontSize: 11 }}>
                                  {src.title} ({src.category})
                                </Tag>
                              ))}
                            </div>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                  <div style={{ marginTop: 8 }}>
                    <Checkbox checked={useKnowledge} onChange={(e) => setUseKnowledge(e.target.checked)}>
                      {t('ai.useKnowledge')}
                    </Checkbox>
                  </div>
                  <Space.Compact style={{ width: '100%', marginTop: 8 }}>
                    <Input
                      placeholder={t('ai.placeholder')}
                      value={input}
                      onChange={(e) => setInput(e.target.value)}
                      onPressEnter={send}
                      disabled={loading}
                    />
                    <Button type="primary" icon={<SendOutlined />} loading={loading} onClick={send}>
                      {t('ai.send')}
                    </Button>
                  </Space.Compact>
                </>
              ),
            },
            {
              key: 'knowledge',
              label: t('ai.knowledge'),
              children: (
                <>
                  <div className="toolbar">
                    <Button type="primary" onClick={() => setKbModalOpen(true)}>{t('ai.addKnowledge')}</Button>
                    <Typography.Text type="secondary">{t('ai.knowledgeNote')}</Typography.Text>
                  </div>
                  <Table
                    rowKey="id"
                    columns={docColumns}
                    dataSource={docs.data?.items ?? []}
                    loading={docs.isLoading}
                    pagination={false}
                  />
                </>
              ),
            },
            {
              key: 'jobs',
              label: t('ai.jobs'),
              children: (
                <List
                  size="small"
                  dataSource={jobs.data?.items ?? []}
                  locale={{ emptyText: t('common.empty') }}
                  renderItem={(job) => {
                    const output = (job.output as { text?: string } | null)?.text ?? ''
                    return (
                      <List.Item
                        actions={[
                          <Button key="a" size="small" onClick={() => review(String(job.id), 'approved')}>{t('ai.approve')}</Button>,
                          <Button key="r" size="small" danger onClick={() => review(String(job.id), 'rejected')}>{t('ai.reject')}</Button>,
                        ]}
                      >
                        <List.Item.Meta
                          title={
                            <Space>
                              <Tag>{String(job.task_type)}</Tag>
                              <Tag color="green">{String(job.output_type ?? '')}</Tag>
                            </Space>
                          }
                          description={<Typography.Text ellipsis style={{ width: 380 }}>{output.slice(0, 120)}</Typography.Text>}
                        />
                      </List.Item>
                    )
                  }}
                />
              ),
            },
          ]}
        />
      </Card>

      <Modal
        open={kbModalOpen}
        title={t('ai.addKnowledge')}
        onCancel={() => setKbModalOpen(false)}
        onOk={async () => {
          const values = await kbForm.validateFields()
          await createDoc(values)
        }}
        width={640}
      >
        <Form form={kbForm} layout="vertical">
          <Form.Item name="title" label={t('common.name')} rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="category" label="分类" initialValue="general">
            <Select options={['种植', '病虫害', '肥料', '农药', '农机', '气象', '土壤', '质量', '法规', '标准', 'general'].map((v) => ({ value: v, label: v }))} />
          </Form.Item>
          <Form.Item name="content" label="内容" rules={[{ required: true }]}>
            <Input.TextArea rows={8} placeholder={t('ai.knowledgePh')} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
