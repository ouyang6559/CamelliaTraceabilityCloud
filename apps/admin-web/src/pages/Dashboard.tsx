import { Card, Col, Row, Statistic, Typography, List, Tag, Timeline } from 'antd'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import ReactECharts from 'echarts-for-react'
import { Link } from 'react-router-dom'
import { getJSON } from '../api/client'

interface Summary {
  farms: number
  plots: number
  crop_batches: number
  tasks_open: number
  devices: number
  devices_online: number
  missions: number
  trace_events: number
  packages: number
  trace_codes: number
  alerts_open: number
  event_trend: { day: string; events: number }[]
  recent_alerts: { id: string; severity: string; status: string; title: string; device_name: string }[]
  upcoming_tasks: { id: string; title: string; status: string; priority: string; plot_name: string }[]
}

const severityColor: Record<string, string> = {
  critical: 'red', warning: 'orange', info: 'blue',
}

export default function Dashboard() {
  const { t } = useTranslation()
  const { data } = useQuery({
    queryKey: ['dashboard-summary'],
    queryFn: () => getJSON<Summary>('/dashboard/summary'),
    refetchInterval: 30_000,
  })

  const trendOption = {
    grid: { left: 40, right: 16, top: 24, bottom: 28 },
    xAxis: { type: 'category', data: (data?.event_trend ?? []).map((d) => String(d.day).slice(5)) },
    yAxis: { type: 'value', minInterval: 1 },
    series: [{ type: 'line', smooth: true, areaStyle: { opacity: 0.15 }, data: (data?.event_trend ?? []).map((d) => d.events), itemStyle: { color: '#2e7d32' } }],
    tooltip: { trigger: 'axis' },
  }

  return (
    <div style={{ padding: 16 }}>
      <Typography.Title level={4} style={{ marginTop: 0 }}>{t('dashboard.title')}</Typography.Title>
      <div className="stat-grid">
        <Card size="small"><Statistic title={t('dashboard.farms')} value={data?.farms ?? '-'} /></Card>
        <Card size="small"><Statistic title={t('dashboard.plots')} value={data?.plots ?? '-'} /></Card>
        <Card size="small"><Statistic title={t('dashboard.tasksOpen')} value={data?.tasks_open ?? '-'} /></Card>
        <Card size="small"><Statistic title={t('dashboard.devicesOnline')} value={`${data?.devices_online ?? '-'} / ${data?.devices ?? '-'}`} /></Card>
        <Card size="small"><Statistic title={t('dashboard.traceEvents')} value={data?.trace_events ?? '-'} /></Card>
        <Card size="small"><Statistic title={t('dashboard.alertsOpen')} value={data?.alerts_open ?? '-'} valueStyle={data && data.alerts_open > 0 ? { color: '#d84315' } : undefined} /></Card>
      </div>

      <Row gutter={16}>
        <Col xs={24} lg={14}>
          <Card title={t('dashboard.eventTrend')} size="small">
            <ReactECharts option={trendOption} style={{ height: 260 }} />
          </Card>
        </Col>
        <Col xs={24} lg={10}>
          <Card title={t('dashboard.recentAlerts')} size="small">
            <List
              size="small"
              dataSource={data?.recent_alerts ?? []}
              locale={{ emptyText: t('common.empty') }}
              renderItem={(a) => (
                <List.Item>
                  <Tag color={severityColor[a.severity] ?? 'default'}>{a.severity}</Tag>
                  <Typography.Text ellipsis style={{ flex: 1 }}>{a.title}</Typography.Text>
                  {a.device_name && <Typography.Text type="secondary">{a.device_name}</Typography.Text>}
                </List.Item>
              )}
            />
            <div style={{ marginTop: 8 }}>
              <Link to="/alerts">{t('nav.alerts')} →</Link>
            </div>
          </Card>
        </Col>
        <Col xs={24} span={24}>
          <Card title={t('dashboard.upcomingTasks')} size="small">
            <Timeline
              items={(data?.upcoming_tasks ?? []).map((task) => ({
                color: task.status === 'in_progress' ? 'green' : 'blue',
                children: (
                  <span>
                    <Tag color={task.priority === 'high' || task.priority === 'urgent' ? 'volcano' : 'default'}>{task.priority}</Tag>
                    <Link to="/agricultural-tasks">{task.title}</Link>
                    {task.plot_name && <Typography.Text type="secondary"> · {task.plot_name}</Typography.Text>}
                  </span>
                ),
              }))}
            />
          </Card>
        </Col>
      </Row>
    </div>
  )
}
