import { useMemo, useState } from 'react'
import { Button, Card, Col, Descriptions, Form, InputNumber, message, Row, Select, Space, Table, Tag, Typography } from 'antd'
import { CloudOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import ReactECharts from 'echarts-for-react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { apiError, getJSON, listPage } from '../api/client'

interface WeatherCurrent {
  time: string
  temperature: number
  humidity: number
  precipitation: number
  weather_code: number
  weather_text: string
  wind_speed: number
  wind_direction: number
  pressure: number
  source: string
}

interface WeatherDay {
  date: string
  temp_max: number
  temp_min: number
  precip_sum: number
  weather_code: number
  weather_text: string
}

interface WeatherResponse {
  location: { lat: number; lng: number }
  current?: WeatherCurrent
  forecast?: { days: WeatherDay[]; source: string }
}

const codeEmoji: Record<number, string> = {
  0: '☀️', 1: '🌤️', 2: '⛅', 3: '☁️', 45: '🌫️', 48: '🌫️',
  51: '🌦️', 53: '🌦️', 55: '🌦️', 61: '🌧️', 63: '🌧️', 65: '🌧️',
  80: '🌧️', 81: '🌧️', 82: '⛈️', 95: '⛈️', 96: '⛈️', 99: '⛈️',
}

function weatherEmoji(code: number): string {
  return codeEmoji[code] ?? '🌡️'
}

/** 气象（Phase 2 IoT 增强，ADR-010）：农场当前天气 + 多日预报（Open-Meteo）。 */
export default function WeatherPage() {
  const { t } = useTranslation()
  const [farmId, setFarmId] = useState<string | undefined>()
  const [lat, setLat] = useState<number | undefined>(28.1925)
  const [lng, setLng] = useState<number | undefined>(113.6285)

  const farms = useQuery({
    queryKey: ['weather-farms'],
    queryFn: () => listPage<Record<string, unknown>>('/farms', { page: 1, page_size: 50 }),
  })

  const current = useQuery({
    queryKey: ['weather-current', lat, lng],
    queryFn: () => getJSON<WeatherResponse>(
      `/weather/current?lat=${lat}&lng=${lng}`,
    ),
    enabled: lat != null && lng != null,
    refetchInterval: 5 * 60_000,
  })

  const forecast = useQuery({
    queryKey: ['weather-forecast', lat, lng],
    queryFn: () => getJSON<WeatherResponse>(
      `/weather/forecast?lat=${lat}&lng=${lng}&days=7`,
    ),
    enabled: lat != null && lng != null,
    refetchInterval: 15 * 60_000,
  })

  function pickFarm(id: string) {
    setFarmId(id)
    const farm = (farms.data?.items ?? []).find((f) => f.id === id)
    const loc = farm?.location as { coordinates?: [number, number] } | null
    if (loc?.coordinates && loc.coordinates.length === 2) {
      setLng(loc.coordinates[0])
      setLat(loc.coordinates[1])
    } else {
      message.warning(t('weather.farmNoLocation'))
    }
  }

  const chartOption = useMemo(() => {
    const days = forecast.data?.forecast?.days ?? []
    return {
      grid: { left: 44, right: 44, top: 32, bottom: 28 },
      tooltip: { trigger: 'axis' },
      legend: { data: [t('weather.tempMax'), t('weather.tempMin'), t('weather.precip')], top: 0 },
      xAxis: { type: 'category', data: days.map((d) => d.date.slice(5)) },
      yAxis: [
        { type: 'value', name: '°C' },
        { type: 'value', name: 'mm', splitLine: { show: false } },
      ],
      series: [
        { name: t('weather.tempMax'), type: 'line', smooth: true, data: days.map((d) => d.temp_max), itemStyle: { color: '#d84315' } },
        { name: t('weather.tempMin'), type: 'line', smooth: true, data: days.map((d) => d.temp_min), itemStyle: { color: '#1565c0' } },
        { name: t('weather.precip'), type: 'bar', yAxisIndex: 1, data: days.map((d) => d.precip_sum), itemStyle: { color: '#42a5f5', opacity: 0.7 } },
      ],
    }
  }, [forecast.data, t])

  const dayColumns: ColumnsType<WeatherDay> = [
    { key: 'date', title: '日期', dataIndex: 'date',
      render: (v: string, row) => <span>{weatherEmoji(row.weather_code)} {v}</span> },
    { key: 'weather_text', title: '天气', dataIndex: 'weather_text' },
    { key: 'temp_max', title: t('weather.tempMax'), dataIndex: 'temp_max', render: (v: number) => `${v}°C` },
    { key: 'temp_min', title: t('weather.tempMin'), dataIndex: 'temp_min', render: (v: number) => `${v}°C` },
    { key: 'precip_sum', title: t('weather.precip'), dataIndex: 'precip_sum', render: (v: number) => `${v} mm` },
  ]

  return (
    <Card className="page-card" title={<span><CloudOutlined /> {t('nav.weather')}</span>}>
      <Card size="small" style={{ marginBottom: 16, background: '#fafafa' }}>
        <Form layout="inline">
          <Form.Item label={t('weather.byFarm')}>
            <Select
              allowClear showSearch optionFilterProp="label"
              style={{ width: 220 }}
              placeholder={t('weather.byFarm')}
              value={farmId}
              onChange={pickFarm}
              options={(farms.data?.items ?? []).map((f) => ({ value: String(f.id), label: String(f.name) }))}
            />
          </Form.Item>
          <Form.Item label="纬度 lat">
            <InputNumber value={lat} onChange={(v) => { setLat(v ?? undefined); setFarmId(undefined) }} style={{ width: 120 }} />
          </Form.Item>
          <Form.Item label="经度 lng">
            <InputNumber value={lng} onChange={(v) => { setLng(v ?? undefined); setFarmId(undefined) }} style={{ width: 120 }} />
          </Form.Item>
          <Button icon={<ReloadOutlined />} onClick={() => { void current.refetch(); void forecast.refetch() }}>
            {t('common.refresh')}
          </Button>
        </Form>
      </Card>

      {current.data?.current ? (
        <Row gutter={16} style={{ marginBottom: 16 }}>
          <Col span={24}>
            <Card size="small" title={
              <Space>
                <span style={{ fontSize: 22 }}>{weatherEmoji(current.data.current.weather_code)}</span>
                <b>{current.data.current.weather_text}</b>
                <Tag>{current.data.current.source}</Tag>
                <Typography.Text type="secondary">{current.data.current.time}</Typography.Text>
              </Space>
            }>
              <Descriptions column={{ xs: 2, md: 4 }} size="small">
                <Descriptions.Item label={t('weather.temperature')}>{current.data.current.temperature}°C</Descriptions.Item>
                <Descriptions.Item label={t('weather.humidity')}>{current.data.current.humidity}%</Descriptions.Item>
                <Descriptions.Item label={t('weather.precip')}>{current.data.current.precipitation} mm</Descriptions.Item>
                <Descriptions.Item label={t('weather.wind')}>
                  {current.data.current.wind_speed} km/h
                </Descriptions.Item>
                <Descriptions.Item label={t('weather.pressure')}>{current.data.current.pressure} hPa</Descriptions.Item>
                <Descriptions.Item label={t('weather.location')}>
                  <span className="mono">{current.data.location.lat.toFixed(4)}, {current.data.location.lng.toFixed(4)}</span>
                </Descriptions.Item>
              </Descriptions>
            </Card>
          </Col>
        </Row>
      ) : null}

      <Row gutter={16}>
        <Col xs={24} lg={14}>
          <Card title={t('weather.forecastChart')} size="small">
            <ReactECharts option={chartOption} style={{ height: 280 }} />
          </Card>
        </Col>
        <Col xs={24} lg={10}>
          <Card title={t('weather.forecastTable')} size="small">
            <Table
              rowKey="date"
              columns={dayColumns}
              dataSource={forecast.data?.forecast?.days ?? []}
              loading={forecast.isLoading}
              pagination={false}
              size="small"
            />
          </Card>
        </Col>
      </Row>
    </Card>
  )
}
