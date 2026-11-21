import { useState } from 'react'
import { Button, Layout, Menu, Dropdown, Select, Typography, Avatar, Space, Grid } from 'antd'
import {
  DashboardOutlined, ContactsOutlined, ClusterOutlined, EnvironmentOutlined,
  ToolOutlined, ThunderboltOutlined, ApiOutlined, RobotOutlined, AlertOutlined,
  SafetyCertificateOutlined, AppstoreOutlined, DropboxOutlined, CarOutlined,
  QrcodeOutlined, FileProtectOutlined, CloudServerOutlined, SyncOutlined,
  ExperimentOutlined, TeamOutlined, SafetyOutlined, FileSearchOutlined,
  HomeOutlined, GlobalOutlined, LineChartOutlined, RadarChartOutlined,
  UserOutlined, LogoutOutlined, MenuFoldOutlined, MenuUnfoldOutlined,
} from '@ant-design/icons'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../stores/auth'
import { clearSession } from '../api/client'

const { Sider, Header, Content } = Layout

/** PC 菜单结构，与 specs/11.2 保持一致（响应式：窄屏自动收起）。 */
export default function AdminLayout() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const user = useAuth((s) => s.user)
  const tenant = useAuth((s) => s.tenant)
  const logout = useAuth((s) => s.logout)
  const locale = useAuth((s) => s.locale)
  const setLocale = useAuth((s) => s.setLocale)
  const [collapsed, setCollapsed] = useState(false)
  const screens = Grid.useBreakpoint()

  const menu = [
    { key: '/', icon: <DashboardOutlined />, label: t('nav.workbench') },
    {
      key: 'production',
      icon: <ContactsOutlined />,
      label: t('nav.production'),
      children: [
        { key: '/farms', icon: <HomeOutlined />, label: t('nav.farms') },
        { key: '/plots', icon: <EnvironmentOutlined />, label: t('nav.plots') },
        { key: '/crops', icon: <ClusterOutlined />, label: t('nav.crops') },
        { key: '/varieties', icon: <ClusterOutlined />, label: t('nav.varieties') },
        { key: '/crop-batches', icon: <AppstoreOutlined />, label: t('nav.cropBatches') },
        { key: '/agricultural-tasks', icon: <ToolOutlined />, label: t('nav.tasks') },
        { key: '/inputs', icon: <ExperimentOutlined />, label: t('nav.inputs') },
        { key: '/input-batches', icon: <ExperimentOutlined />, label: t('nav.inputBatches') },
        { key: '/input-usages', icon: <ExperimentOutlined />, label: t('nav.inputUsages') },
        { key: '/disease-records', icon: <AlertOutlined />, label: t('nav.diseases') },
      ],
    },
    {
      key: 'devices',
      icon: <ThunderboltOutlined />,
      label: t('nav.devices'),
      children: [
        { key: '/device-models', icon: <ApiOutlined />, label: t('nav.deviceModels') },
        { key: '/devices', icon: <ApiOutlined />, label: t('nav.devices') },
        { key: '/device-map', icon: <GlobalOutlined />, label: t('nav.deviceMap') },
        { key: '/missions', icon: <RobotOutlined />, label: t('nav.missions') },
        { key: '/geofences', icon: <SafetyOutlined />, label: t('nav.geofences') },
        { key: '/sensors', icon: <RadarChartOutlined />, label: t('nav.sensors') },
        { key: '/datastreams', icon: <LineChartOutlined />, label: t('nav.datastreams') },
        { key: '/observations', icon: <LineChartOutlined />, label: t('nav.observations') },
        { key: '/alerts', icon: <AlertOutlined />, label: t('nav.alerts') },
        { key: '/alert-rules', icon: <AlertOutlined />, label: t('nav.alertRules') },
      ],
    },
    {
      key: 'processing',
      icon: <DropboxOutlined />,
      label: t('nav.rawBatches'),
      children: [
        { key: '/harvests', icon: <ToolOutlined />, label: t('nav.harvests') },
        { key: '/raw-material-batches', icon: <DropboxOutlined />, label: t('nav.rawBatches') },
        { key: '/processing-batches', icon: <AppstoreOutlined />, label: t('nav.processingBatches') },
        { key: '/products', icon: <AppstoreOutlined />, label: t('nav.products') },
        { key: '/product-batches', icon: <AppstoreOutlined />, label: t('nav.productBatches') },
        { key: '/packages', icon: <QrcodeOutlined />, label: t('nav.packages') },
        { key: '/warehouses', icon: <HomeOutlined />, label: t('nav.warehouses') },
        { key: '/inventory', icon: <DropboxOutlined />, label: t('nav.inventory') },
        { key: '/logistics', icon: <CarOutlined />, label: t('nav.logistics') },
        { key: '/quality-tests', icon: <FileProtectOutlined />, label: t('nav.qualityTests') },
        { key: '/quality-reports', icon: <FileProtectOutlined />, label: t('nav.qualityReports') },
      ],
    },
    {
      key: 'trace',
      icon: <QrcodeOutlined />,
      label: t('nav.traceCodes'),
      children: [
        { key: '/trace-events', icon: <FileSearchOutlined />, label: t('nav.traceEvents') },
        { key: '/trace-codes', icon: <QrcodeOutlined />, label: t('nav.traceCodes') },
        { key: '/evidence', icon: <FileProtectOutlined />, label: t('nav.evidence') },
        { key: '/blockchain-proofs', icon: <SafetyCertificateOutlined />, label: t('nav.proofs') },
        { key: '/external-platforms', icon: <CloudServerOutlined />, label: t('nav.platforms') },
        { key: '/sync-tasks', icon: <SyncOutlined />, label: t('nav.syncTasks') },
      ],
    },
    { key: '/ai', icon: <RobotOutlined />, label: t('nav.ai') },
    ...(user?.platform_admin
      ? [{ key: '/platform/tenants', icon: <CloudServerOutlined />, label: t('nav.platformAdmin') }]
      : []),
    {
      key: 'system',
      icon: <TeamOutlined />,
      label: t('nav.system'),
      children: [
        { key: '/users', icon: <UserOutlined />, label: t('nav.users') },
        { key: '/roles', icon: <SafetyOutlined />, label: t('nav.roles') },
        { key: '/audit-logs', icon: <FileSearchOutlined />, label: t('nav.auditLogs') },
        { key: '/tenant', icon: <CloudServerOutlined />, label: t('nav.tenant') },
      ],
    },
  ]

  const userMenu = {
    items: [
      { key: 'locale', label: (
        <Select
          size="small"
          value={locale}
          style={{ width: 120 }}
          onChange={(v) => { setLocale(v); void i18n.changeLanguage(v) }}
          options={[
            { value: 'zh-CN', label: '中文' },
            { value: 'en-US', label: 'English' },
          ]}
        />
      ) },
      { type: 'divider' as const },
      { key: 'logout', icon: <LogoutOutlined />, label: t('common.logout') },
    ],
    onClick: ({ key }: { key: string }) => {
      if (key === 'logout') {
        void fetch('/api/v1/auth/logout', { method: 'POST' }).catch(() => undefined)
        clearSession()
        logout()
        navigate('/login')
      }
    },
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        collapsible
        collapsed={collapsed || !screens.md}
        trigger={null}
        theme="dark"
        width={228}
        style={{ display: 'flex', flexDirection: 'column' }}
      >
        <div style={{ height: 56, display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 10, padding: '0 8px' }}>
          <img src="/logo.svg" alt="CTC logo" style={{ width: 30, height: 30, borderRadius: 8, flexShrink: 0 }} />
          {!collapsed && screens.md && (
            <span style={{ color: '#fff', fontSize: 15, fontWeight: 600, whiteSpace: 'nowrap', overflow: 'hidden' }}>
              {t('app.name')}
            </span>
          )}
        </div>
        <Menu
          theme="dark"
          mode="inline"
          items={menu}
          selectedKeys={[location.pathname]}
          defaultOpenKeys={screens.md ? ['production', 'devices', 'processing', 'trace'] : []}
          onClick={({ key }) => navigate(String(key))}
          style={{ flex: 1, overflowY: 'auto' }}
        />
        {!collapsed && screens.md && (
          <div style={{
            color: 'rgba(255,255,255,.45)', fontSize: 11, textAlign: 'center',
            padding: '10px 0 14px', userSelect: 'none',
          }}>
            CTC · v0.1.0
          </div>
        )}
      </Sider>
      <Layout>
        <Header style={{
          background: '#fff', padding: '0 16px', display: 'flex',
          alignItems: 'center', justifyContent: 'space-between',
          borderBottom: '1px solid #f0f0f0', height: 56,
        }}>
          <Space>
            <Button
              type="text"
              icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
              onClick={() => setCollapsed(!collapsed)}
            />
            {tenant && (
              <Typography.Text
                style={{
                  background: '#e8f5e9', color: '#2e7d32', borderRadius: 10,
                  padding: '2px 10px', fontSize: 12, fontWeight: 600,
                }}
              >
                {tenant.name || tenant.code}
              </Typography.Text>
            )}
            <Typography.Text type="secondary">{t('app.subtitle')}</Typography.Text>
          </Space>
          <Dropdown menu={userMenu} placement="bottomRight">
            <Space style={{ cursor: 'pointer' }}>
              <Avatar style={{ backgroundColor: '#2e7d32' }} size="small">
                {(user?.display_name || user?.username || 'U').slice(0, 1).toUpperCase()}
              </Avatar>
              <Typography.Text>{user?.display_name || user?.username}</Typography.Text>
            </Space>
          </Dropdown>
        </Header>
        <Content style={{ overflow: 'auto' }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  )
}
