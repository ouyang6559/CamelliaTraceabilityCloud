import { useState } from 'react'
import { Alert, Avatar, Button, Card, Input, List, message, Space, Tag, Typography } from 'antd'
import { RobotOutlined, SendOutlined, UserOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiError, getJSON, listPage, postJSON } from '../api/client'

interface ChatEntry { role: 'user' | 'ai'; content: string; type?: string; confidence?: number }

/** AI 农业助手：对话输出全部标注为 AI 生成建议（specs/07.8 禁止自动执行）。 */
export default function AiAssistant() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [messages, setMessages] = useState<ChatEntry[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)

  const jobs = useQuery({
    queryKey: ['ai-jobs'],
    queryFn: () => listPage<Record<string, unknown>>('/ai/jobs', { page: 1, page_size: 10 }),
    refetchInterval: 20_000,
  })

  async function send() {
    const text = input.trim()
    if (!text) return
    setInput('')
    setMessages((m) => [...m, { role: 'user', content: text }])
    setLoading(true)
    try {
      const res = await postJSON<{ output: string; output_type: string; confidence: number }>('/ai/chat', {
        messages: [{ role: 'user', content: text }],
      })
      setMessages((m) => [...m, {
        role: 'ai', content: res.output,
        type: res.output_type, confidence: res.confidence,
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

  return (
    <div style={{ padding: 16 }}>
      <Alert type="info" showIcon message={t('ai.disclaimer')} style={{ marginBottom: 16 }} />
      <Space align="start" style={{ width: '100%' }} size={16}>
        <Card title={t('ai.chat')} style={{ width: 560 }} size="small">
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
                </div>
              </div>
            ))}
          </div>
          <Space.Compact style={{ width: '100%', marginTop: 12 }}>
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
        </Card>

        <Card title={t('ai.jobs')} style={{ width: 420 }} size="small">
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
                    description={<Typography.Text ellipsis style={{ width: 220 }}>{output.slice(0, 80)}</Typography.Text>}
                  />
                </List.Item>
              )
            }}
          />
        </Card>
      </Space>
    </div>
  )
}
