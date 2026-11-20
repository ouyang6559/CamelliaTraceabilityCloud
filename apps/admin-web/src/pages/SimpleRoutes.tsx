import { Navigate, Route, Routes } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import CrudPage from '../components/CrudPage'

interface S { label: string; value: string }
const yesNo: S[] = [
  { label: '是 / Yes', value: 'true' },
  { label: '否 / No', value: 'false' },
]

/**
 * Uniform CRUD pages for the generic REST resources. Column definitions mirror
 * the backend SELECT lists; status columns use the shared i18n status map.
 */
export function SimpleResource({ kind }: { kind: string }) {
  const { t } = useTranslation()
  const statusRender = (row: Record<string, unknown>) => {
    const v = String(row.status ?? '')
    return v ? t(`status.${v}`) : '-'
  }

  switch (kind) {
    case 'crops':
      return (
        <CrudPage
          resourcePath="/crops" perm="crop" title={t('nav.crops')}
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'code', title: t('common.code') },
            { key: 'category', title: '类别' },
            { key: 'description', title: t('common.description'), render: (r) => String(r.description ?? '-').slice(0, 40) },
          ]}
          fields={[
            { name: 'name', label: t('common.name'), required: true },
            { name: 'code', label: t('common.code') },
            { name: 'category', label: '类别', type: 'select', options: [
              { value: 'oil-tea', label: '油茶' }, { value: 'vegetable', label: '蔬菜' },
              { value: 'fruit', label: '水果' }, { value: 'grain', label: '谷物' },
            ] },
            { name: 'description', label: t('common.description'), type: 'textarea' },
          ]}
        />
      )
    case 'varieties':
      return (
        <CrudPage
          resourcePath="/varieties" perm="variety" title={t('nav.varieties')}
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'code', title: t('common.code') },
            { key: 'crop_id', title: t('nav.crops') },
          ]}
          fields={[
            { name: 'crop_id', label: '作物 ID (crops)', required: true },
            { name: 'name', label: t('common.name'), required: true },
            { name: 'code', label: t('common.code') },
            { name: 'description', label: t('common.description'), type: 'textarea' },
          ]}
        />
      )
    case 'crop-batches':
      return (
        <CrudPage
          resourcePath="/crop-batches" perm="crop-batch" title={t('nav.cropBatches')}
          filters={[
            { name: 'status', label: t('common.status'), options: [
              { value: 'growing', label: t('status.growing') },
              { value: 'harvested', label: t('status.harvested') },
              { value: 'closed', label: t('status.cancelled') },
            ] },
          ]}
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'code', title: t('common.code') },
            { key: 'plot_id', title: t('nav.plots') },
            { key: 'crop_id', title: t('nav.crops') },
            { key: 'planted_at', title: '定植时间' },
            { key: 'area_m2', title: '面积 m²', render: (r) => Number(r.area_m2 ?? 0).toLocaleString() },
            { key: 'status', title: t('common.status'), render: statusRender },
          ]}
          fields={[
            { name: 'plot_id', label: '地块 ID (plots)', required: true },
            { name: 'crop_id', label: '作物 ID (crops)', required: true },
            { name: 'variety_id', label: '品种 ID (可选)' },
            { name: 'name', label: t('common.name'), required: true },
            { name: 'code', label: t('common.code') },
            { name: 'planted_at', label: '定植时间 (ISO-8601)' },
            { name: 'area_m2', label: '面积 m²', type: 'number' },
          ]}
        />
      )
    case 'inputs':
      return (
        <CrudPage
          resourcePath="/inputs" perm="input" title={t('nav.inputs')}
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'code', title: t('common.code') },
            { key: 'category', title: '类别' },
            { key: 'unit', title: '单位' },
            { key: 'spec', title: '规格' },
          ]}
          fields={[
            { name: 'name', label: t('common.name'), required: true },
            { name: 'code', label: t('common.code') },
            { name: 'category', label: '类别', type: 'select', options: [
              { value: 'fertilizer', label: '肥料' }, { value: 'pesticide', label: '农药' },
              { value: 'seed', label: '种子/种苗' }, { value: 'other', label: '其他' },
            ] },
            { name: 'unit', label: '单位', initialValue: 'kg' },
            { name: 'spec', label: '规格' },
            { name: 'description', label: t('common.description'), type: 'textarea' },
          ]}
        />
      )
    case 'input-batches':
      return (
        <CrudPage
          resourcePath="/input-batches" perm="input-batch" title={t('nav.inputBatches')}
          columns={[
            { key: 'code', title: t('common.code') },
            { key: 'input_id', title: t('nav.inputs') },
            { key: 'supplier', title: '供应商' },
            { key: 'quantity', title: '库存数量', render: (r) => Number(r.quantity ?? 0) },
            { key: 'unit', title: '单位' },
            { key: 'expired_at', title: '有效期至' },
            { key: 'status', title: t('common.status'), render: statusRender },
          ]}
          fields={[
            { name: 'input_id', label: '投入品 ID (inputs)', required: true },
            { name: 'code', label: t('common.code'), required: true },
            { name: 'supplier', label: '供应商' },
            { name: 'quantity', label: '数量', type: 'number' },
            { name: 'unit', label: '单位' },
            { name: 'produced_at', label: '生产日期 (ISO-8601)' },
            { name: 'expired_at', label: '有效期至 (ISO-8601)' },
          ]}
        />
      )
    case 'input-usages':
      return (
        <CrudPage
          resourcePath="/input-usages" perm="input-usage" title={t('nav.inputUsages')}
          readOnly
          columns={[
            { key: 'input_batch_code', title: '投入品批次' },
            { key: 'input_name', title: t('nav.inputs') },
            { key: 'task_id', title: t('nav.tasks') },
            { key: 'quantity', title: '用量', render: (r) => Number(r.quantity ?? 0) },
            { key: 'unit', title: '单位' },
            { key: 'applied_at', title: '使用时间' },
          ]}
        />
      )
    case 'disease-records':
      return (
        <CrudPage
          resourcePath="/disease-records" perm="disease-record" title={t('nav.diseases')}
          filters={[
            { name: 'category', label: '类别', options: [
              { value: 'disease', label: '病害' }, { value: 'pest', label: '虫害' }, { value: 'weed', label: '草害' },
            ] },
            { name: 'status', label: t('common.status'), options: [
              { value: 'open', label: '待处理' }, { value: 'treating', label: '处置中' }, { value: 'resolved', label: t('status.resolved') },
            ] },
          ]}
          columns={[
            { key: 'name_zh', title: '名称' },
            { key: 'name_en', title: 'Name (EN)' },
            { key: 'category', title: '类别' },
            { key: 'severity', title: '严重程度' },
            { key: 'plot_id', title: t('nav.plots') },
            { key: 'discovered_at', title: '发现时间' },
            { key: 'status', title: t('common.status'), render: statusRender },
          ]}
          fields={[
            { name: 'name_zh', label: '名称（中文）', required: true },
            { name: 'name_en', label: 'Name (EN)' },
            { name: 'category', label: '类别', type: 'select', options: [
              { value: 'disease', label: '病害' }, { value: 'pest', label: '虫害' }, { value: 'weed', label: '草害' },
            ] },
            { name: 'plot_id', label: '地块 ID (可选)' },
            { name: 'crop_batch_id', label: '种植批次 ID (可选)' },
            { name: 'severity', label: '严重程度', type: 'select', options: [
              { value: 'low', label: '低' }, { value: 'medium', label: '中' }, { value: 'high', label: '高' },
            ] },
            { name: 'treatment', label: '处置措施', type: 'textarea' },
            { name: 'status', label: t('common.status'), type: 'select', options: [
              { value: 'open', label: '待处理' }, { value: 'treating', label: '处置中' }, { value: 'resolved', label: t('status.resolved') },
            ] },
          ]}
        />
      )
    case 'device-models':
      return (
        <CrudPage
          resourcePath="/device-models" perm="device-model" title={t('nav.deviceModels')}
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'vendor', title: '厂商' },
            { key: 'device_type', title: '设备类型' },
            { key: 'capabilities', title: t('device.capabilities'), render: (r) => String(JSON.stringify(r.capabilities ?? [])).slice(0, 60) },
          ]}
          fields={[
            { name: 'name', label: t('common.name'), required: true },
            { name: 'vendor', label: '厂商' },
            { name: 'device_type', label: '设备类型', type: 'select', options: [
              { value: 'UAV', label: '无人机 UAV' }, { value: 'UGV', label: '无人车 UGV' },
              { value: 'TRACTOR', label: '拖拉机' }, { value: 'MOWER', label: '割草机器人' },
              { value: 'SPRAYER', label: '喷药机' }, { value: 'WEATHER_STATION', label: '气象站' },
              { value: 'SOIL_SENSOR', label: '土壤传感器' }, { value: 'CAMERA', label: '摄像头' },
              { value: 'GATEWAY', label: '网关' },
            ] },
            { name: 'capabilities', label: '能力（JSON 数组，如 ["TELEMETRY","GPS","MISSION"]）', type: 'json' },
          ]}
        />
      )
    case 'devices':
      return (
        <CrudPage
          resourcePath="/devices" perm="device" title={t('nav.devices')}
          filters={[
            { name: 'status', label: t('common.status'), options: [
              { value: 'online', label: t('status.online') }, { value: 'offline', label: t('status.offline') },
              { value: 'error', label: t('status.error') }, { value: 'maintenance', label: t('status.maintenance') },
            ] },
          ]}
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'code', title: '编号 SN' },
            { key: 'device_type', title: '类型' },
            { key: 'status', title: t('common.status'), render: statusRender },
            { key: 'battery_level', title: '电量%', render: (r) => (r.battery_level == null ? '-' : String(r.battery_level)) },
            { key: 'last_seen_at', title: '最后上线' },
          ]}
          fields={[
            { name: 'name', label: t('common.name'), required: true },
            { name: 'code', label: '编号 SN' },
            { name: 'device_type', label: '设备类型', type: 'select', options: [
              { value: 'UAV', label: '无人机' }, { value: 'MOWER', label: '割草机器人' },
              { value: 'WEATHER_STATION', label: '气象站' }, { value: 'SOIL_SENSOR', label: '土壤传感器' },
              { value: 'GATEWAY', label: '网关' },
            ] },
            { name: 'model_id', label: '型号 ID (可选)' },
            { name: 'capabilities', label: '能力（JSON 数组）', type: 'json' },
            { name: 'latitude', label: '纬度', type: 'number' },
            { name: 'longitude', label: '经度', type: 'number' },
            { name: 'farm_id', label: '基地 ID (可选)' },
            { name: 'plot_id', label: '地块 ID (可选)' },
          ]}
        />
      )
    case 'geofences':
      return (
        <CrudPage
          resourcePath="/geofences" perm="geofence" title={t('nav.geofences')}
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'fence_type', title: '类型', render: (r) => (r.fence_type === 'keep_out' ? '禁飞/禁入' : '作业区') },
            { key: 'farm_id', title: t('nav.farms') },
            { key: 'active', title: '启用', render: (r) => (r.active ? '✅' : '—') },
          ]}
          fields={[
            { name: 'name', label: t('common.name'), required: true },
            { name: 'fence_type', label: '类型', type: 'select', options: [
              { value: 'keep_out', label: '禁飞/禁入区' }, { value: 'work_area', label: '作业区' },
            ] },
            { name: 'farm_id', label: '基地 ID (可选)' },
            { name: 'boundary', label: '边界 GeoJSON (Polygon)', type: 'json', required: true },
          ]}
        />
      )
    case 'sensors':
      return (
        <CrudPage
          resourcePath="/sensors" perm="observation" title={t('nav.sensors')} readOnly
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'device_id', title: t('nav.devices') },
            { key: 'device_name', title: '设备名称' },
            { key: 'description', title: t('common.description') },
          ]}
        />
      )
    case 'datastreams':
      return (
        <CrudPage
          resourcePath="/datastreams" perm="observation" title={t('nav.datastreams')} readOnly
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'observed_property', title: '观测属性' },
            { key: 'unit', title: '单位' },
            { key: 'device_name', title: '设备名称' },
          ]}
        />
      )
    case 'observations':
      return (
        <CrudPage
          resourcePath="/observations" perm="observation" title={t('nav.observations')} readOnly
          columns={[
            { key: 'observed_property', title: '观测属性' },
            { key: 'result_number', title: '数值', render: (r) => (r.result_number == null ? '-' : String(r.result_number)) },
            { key: 'unit', title: '单位' },
            { key: 'device_id', title: t('nav.devices') },
            { key: 'phenomenon_time', title: '观测时间' },
          ]}
        />
      )
    case 'alerts':
      return (
        <CrudPage
          resourcePath="/alerts" perm="alert" title={t('nav.alerts')} noEdit
          filters={[
            { name: 'status', label: t('common.status'), options: [
              { value: 'open', label: t('status.open') }, { value: 'acknowledged', label: t('status.acknowledged') },
              { value: 'resolved', label: t('status.resolved') },
            ] },
          ]}
          columns={[
            { key: 'title', title: '告警' },
            { key: 'rule_name', title: '规则' },
            { key: 'device_name', title: t('nav.devices') },
            { key: 'severity', title: '级别' },
            { key: 'status', title: t('common.status'), render: statusRender },
            { key: 'triggered_at', title: '触发时间' },
          ]}
        />
      )
    case 'alert-rules':
      return (
        <CrudPage
          resourcePath="/alert-rules" perm="alert-rule" title={t('nav.alertRules')}
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'device_type', title: '设备类型' },
            { key: 'metric', title: '指标' },
            { key: 'operator', title: '比较' },
            { key: 'threshold', title: '阈值', render: (r) => String(r.threshold) },
            { key: 'severity', title: '级别' },
            { key: 'enabled', title: '启用', render: (r) => (r.enabled ? '✅' : '—') },
          ]}
          fields={[
            { name: 'name', label: t('common.name'), required: true },
            { name: 'device_type', label: '设备类型（留空匹配全部）' },
            { name: 'metric', label: '遥测指标 (如 temperature)', required: true },
            { name: 'operator', label: '比较符', type: 'select', initialValue: '>', options: ['>', '>=', '<', '<=', '=='].map((v) => ({ value: v, label: v })) },
            { name: 'threshold', label: '阈值', type: 'number', required: true },
            { name: 'severity', label: '级别', type: 'select', initialValue: 'warning', options: [
              { value: 'info', label: 'info' }, { value: 'warning', label: 'warning' }, { value: 'critical', label: 'critical' },
            ] },
          ]}
        />
      )
    case 'harvests':
      return (
        <CrudPage
          resourcePath="/harvests" perm="harvest" title={t('nav.harvests')}
          columns={[
            { key: 'plot_id', title: t('nav.plots') },
            { key: 'crop_batch_id', title: t('nav.cropBatches') },
            { key: 'harvested_at', title: '采收时间' },
            { key: 'quantity', title: '数量', render: (r) => Number(r.quantity ?? 0) },
            { key: 'unit', title: '单位' },
            { key: 'operator_id', title: '操作人' },
          ]}
          fields={[
            { name: 'plot_id', label: '地块 ID (plots)', required: true },
            { name: 'crop_batch_id', label: '种植批次 ID (可选)' },
            { name: 'harvested_at', label: '采收时间 (ISO-8601)' },
            { name: 'quantity', label: '数量', type: 'number' },
            { name: 'unit', label: '单位', initialValue: 'kg' },
            { name: 'note', label: t('common.description'), type: 'textarea' },
          ]}
        />
      )
    case 'raw-material-batches':
      return (
        <CrudPage
          resourcePath="/raw-material-batches" perm="raw-material-batch" title={t('nav.rawBatches')}
          columns={[
            { key: 'code', title: t('common.code') },
            { key: 'harvest_id', title: t('nav.harvests') },
            { key: 'quantity', title: '数量', render: (r) => Number(r.quantity ?? 0) },
            { key: 'unit', title: '单位' },
            { key: 'received_at', title: '入库时间' },
            { key: 'status', title: t('common.status'), render: statusRender },
          ]}
          fields={[
            { name: 'code', label: t('common.code'), required: true },
            { name: 'harvest_id', label: '采收 ID (harvests)' },
            { name: 'quantity', label: '数量', type: 'number' },
            { name: 'unit', label: '单位', initialValue: 'kg' },
            { name: 'note', label: t('common.description'), type: 'textarea' },
          ]}
        />
      )
    case 'processing-batches':
      return (
        <CrudPage
          resourcePath="/processing-batches" perm="processing-batch" title={t('nav.processingBatches')}
          columns={[
            { key: 'code', title: t('common.code') },
            { key: 'raw_material_batch_id', title: t('nav.rawBatches') },
            { key: 'process_type', title: '工艺' },
            { key: 'input_quantity', title: '投入', render: (r) => Number(r.input_quantity ?? 0) },
            { key: 'output_quantity', title: '产出', render: (r) => Number(r.output_quantity ?? 0) },
            { key: 'status', title: t('common.status'), render: statusRender },
          ]}
          fields={[
            { name: 'code', label: t('common.code'), required: true },
            { name: 'raw_material_batch_id', label: '原料批次 ID (raw-material-batches)', required: true },
            { name: 'process_type', label: '工艺', type: 'select', options: [
              { value: 'drying', label: '干燥' }, { value: 'shelling', label: '脱壳' },
              { value: 'pressing', label: '压榨' }, { value: 'refining', label: '精炼' }, { value: 'packing', label: '包装' },
            ] },
            { name: 'input_quantity', label: '投入量', type: 'number' },
            { name: 'unit', label: '单位', initialValue: 'kg' },
            { name: 'parameters', label: '工艺参数 (JSON)', type: 'json' },
          ]}
        />
      )
    case 'products':
      return (
        <CrudPage
          resourcePath="/products" perm="product" title={t('nav.products')}
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'code', title: t('common.code') },
            { key: 'category', title: '类别' },
            { key: 'spec', title: '规格' },
            { key: 'unit', title: '单位' },
          ]}
          fields={[
            { name: 'name', label: t('common.name'), required: true },
            { name: 'code', label: t('common.code') },
            { name: 'category', label: '类别', initialValue: 'camellia-oil' },
            { name: 'spec', label: '规格' },
            { name: 'unit', label: '单位', initialValue: 'bottle' },
            { name: 'description', label: t('common.description'), type: 'textarea' },
          ]}
        />
      )
    case 'product-batches':
      return (
        <CrudPage
          resourcePath="/product-batches" perm="product-batch" title={t('nav.productBatches')}
          columns={[
            { key: 'code', title: t('common.code') },
            { key: 'product_id', title: t('nav.products') },
            { key: 'processing_batch_id', title: t('nav.processingBatches') },
            { key: 'produced_at', title: '生产日期' },
            { key: 'quantity', title: '数量', render: (r) => Number(r.quantity ?? 0) },
            { key: 'unit', title: '单位' },
            { key: 'status', title: t('common.status'), render: statusRender },
          ]}
          fields={[
            { name: 'code', label: t('common.code'), required: true },
            { name: 'product_id', label: '产品 ID (products)', required: true },
            { name: 'processing_batch_id', label: '加工批次 ID (可选)' },
            { name: 'produced_at', label: '生产日期 (ISO-8601)' },
            { name: 'quantity', label: '数量', type: 'number' },
            { name: 'unit', label: '单位', initialValue: 'bottle' },
          ]}
        />
      )
    case 'warehouses':
      return (
        <CrudPage
          resourcePath="/warehouses" perm="warehouse" title={t('nav.warehouses')}
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'code', title: t('common.code') },
            { key: 'address', title: '地址' },
            { key: 'capacity', title: '容量', render: (r) => (r.capacity == null ? '-' : String(r.capacity)) },
            { key: 'unit', title: '单位' },
          ]}
          fields={[
            { name: 'name', label: t('common.name'), required: true },
            { name: 'code', label: t('common.code') },
            { name: 'address', label: '地址' },
            { name: 'capacity', label: '容量', type: 'number' },
            { name: 'unit', label: '单位', initialValue: 'kg' },
          ]}
        />
      )
    case 'inventory':
      return (
        <CrudPage
          resourcePath="/inventory" perm="inventory" title={t('nav.inventory')} noEdit
          columns={[
            { key: 'warehouse_name', title: '仓库' },
            { key: 'ref_type', title: '对象类型' },
            { key: 'ref_id', title: '对象 ID' },
            { key: 'quantity', title: '数量', render: (r) => Number(r.quantity ?? 0) },
            { key: 'unit', title: '单位' },
            { key: 'updated_at', title: t('common.updatedAt') },
          ]}
          fields={[
            { name: 'warehouse_id', label: '仓库 ID (warehouses)', required: true },
            { name: 'ref_type', label: '对象类型', type: 'select', options: [
              { value: 'product_batch', label: '产品批次' }, { value: 'package', label: '包装' },
            ] },
            { name: 'ref_id', label: '对象 ID', required: true },
            { name: 'direction', label: '方向', type: 'select', options: [
              { value: 'in', label: '入库' }, { value: 'out', label: '出库' }, { value: 'adjust', label: '调整' },
            ] },
            { name: 'quantity', label: '数量', type: 'number', required: true },
            { name: 'reason', label: '原因' },
          ]}
        />
      )
    case 'logistics':
      return (
        <CrudPage
          resourcePath="/logistics" perm="logistics" title={t('nav.logistics')}
          columns={[
            { key: 'package_id', title: t('nav.packages') },
            { key: 'carrier', title: '承运方' },
            { key: 'tracking_no', title: '运单号' },
            { key: 'origin', title: '起点' },
            { key: 'destination', title: '终点' },
            { key: 'status', title: t('common.status'), render: statusRender },
          ]}
          fields={[
            { name: 'package_id', label: '包装 ID (packages)' },
            { name: 'carrier', label: '承运方' },
            { name: 'tracking_no', label: '运单号' },
            { name: 'origin', label: '起点' },
            { name: 'destination', label: '终点' },
            { name: 'departed_at', label: '发运时间 (ISO-8601)' },
            { name: 'arrived_at', label: '到达时间 (ISO-8601)' },
          ]}
        />
      )
    case 'quality-tests':
      return (
        <CrudPage
          resourcePath="/quality-tests" perm="quality-test" title={t('nav.qualityTests')}
          columns={[
            { key: 'ref_type', title: '对象类型' },
            { key: 'ref_id', title: '对象 ID' },
            { key: 'item', title: '检测项目' },
            { key: 'result_value', title: '结果', render: (r) => String(r.result_value ?? r.result_text ?? '-') },
            { key: 'unit', title: '单位' },
            { key: 'passed', title: '合格', render: (r) => (r.passed ? '✅' : '❌') },
            { key: 'tested_at', title: '检测时间' },
          ]}
          fields={[
            { name: 'ref_type', label: '对象类型', type: 'select', initialValue: 'product_batch', options: [
              { value: 'product_batch', label: '产品批次' }, { value: 'raw_material_batch', label: '原料批次' },
              { value: 'processing_batch', label: '加工批次' },
            ] },
            { name: 'ref_id', label: '对象 ID', required: true },
            { name: 'item', label: '检测项目 (如 acid_value)', required: true },
            { name: 'standard', label: '标准 (如 GB/T 11765-2018)' },
            { name: 'result_value', label: '数值结果', type: 'number' },
            { name: 'result_text', label: '文本结果' },
            { name: 'unit', label: '单位' },
            { name: 'passed', label: '是否合格', type: 'select', initialValue: 'true', options: yesNo },
            { name: 'note', label: t('common.description'), type: 'textarea' },
          ]}
        />
      )
    case 'quality-reports':
      return (
        <CrudPage
          resourcePath="/quality-reports" perm="quality-report" title={t('nav.qualityReports')}
          columns={[
            { key: 'code', title: t('common.code') },
            { key: 'title', title: t('common.name') },
            { key: 'ref_id', title: '对象 ID' },
            { key: 'conclusion', title: '结论' },
            { key: 'tester', title: '检测人' },
            { key: 'tested_at', title: '检测时间' },
          ]}
          fields={[
            { name: 'code', label: t('common.code'), required: true },
            { name: 'title', label: t('common.name'), required: true },
            { name: 'ref_type', label: '对象类型', initialValue: 'product_batch' },
            { name: 'ref_id', label: '对象 ID' },
            { name: 'conclusion', label: '结论', type: 'select', options: [
              { value: 'pass', label: '合格' }, { value: 'fail', label: '不合格' }, { value: 'conditional', label: '有条件放行' },
            ] },
            { name: 'tester', label: '检测人' },
            { name: 'items', label: '检测项 (JSON 数组)', type: 'json' },
          ]}
        />
      )
    case 'evidence':
      return (
        <CrudPage
          resourcePath="/evidence" perm="evidence" title={t('nav.evidence')}
          columns={[
            { key: 'evidence_type', title: '类型' },
            { key: 'ref_type', title: '对象类型' },
            { key: 'ref_id', title: '对象 ID' },
            { key: 'filename', title: '文件' },
            { key: 'sha256', title: 'SHA-256', render: (r) => <span className="mono">{String(r.sha256 ?? '').slice(0, 18)}…</span> },
            { key: 'captured_at', title: '取证时间' },
          ]}
        />
      )
    case 'blockchain-proofs':
      return (
        <CrudPage
          resourcePath="/blockchain-proofs" perm="blockchain-proof" title={t('nav.proofs')}
          columns={[
            { key: 'ref_type', title: '对象类型' },
            { key: 'ref_id', title: '对象 ID' },
            { key: 'provider', title: '存证平台' },
            { key: 'algorithm', title: '算法' },
            { key: 'hash', title: 'Hash', render: (r) => <span className="mono">{String(r.hash ?? '').slice(0, 18)}…</span> },
            { key: 'provider_tx_id', title: 'Tx ID', render: (r) => <span className="mono">{String(r.provider_tx_id ?? '').slice(0, 18)}…</span> },
            { key: 'status', title: t('common.status') },
          ]}
        />
      )
    case 'external-platforms':
      return (
        <CrudPage
          resourcePath="/external-platforms" perm="platform" title={t('nav.platforms')}
          columns={[
            { key: 'name', title: t('common.name') },
            { key: 'provider_type', title: 'Provider' },
            { key: 'status', title: t('common.status') },
            { key: 'capabilities', title: '能力', render: (r) => String(JSON.stringify(r.capabilities ?? [])) },
          ]}
          fields={[
            { name: 'name', label: t('common.name'), required: true },
            { name: 'provider_type', label: 'Provider 类型', type: 'select', options: [
              { value: 'mock', label: 'mock（演示）' },
              { value: 'national', label: '国家平台（需官方 API）' },
              { value: 'hubei', label: '湖北平台（需官方 API）' },
              { value: 'himarking', label: 'HiMarking（需商业授权）' },
              { value: 'antchain', label: '蚂蚁链（需授权）' },
              { value: 'tencent_zxin', label: '腾讯至信链（需授权）' },
              { value: 'nongzhan', label: '农展网（需合作接口）' },
              { value: 'lifei', label: '立非智码（需接口文档）' },
            ] },
            { name: 'capabilities', label: '能力 (JSON 数组)', type: 'json' },
          ]}
        />
      )
    case 'sync-tasks':
      return (
        <CrudPage
          resourcePath="/sync-tasks" perm="sync-task" title={t('nav.syncTasks')}
          filters={[
            { name: 'status', label: t('common.status'), options: [
              { value: 'PENDING', label: t('status.PENDING') }, { value: 'SYNCED', label: t('status.SYNCED') },
              { value: 'FAILED', label: t('status.FAILED') }, { value: 'RETRYING', label: t('status.RETRYING') },
              { value: 'LOCAL_ONLY', label: t('status.LOCAL_ONLY') },
            ] },
          ]}
          columns={[
            { key: 'platform_name', title: t('nav.platforms') },
            { key: 'ref_type', title: '对象类型' },
            { key: 'ref_id', title: '对象 ID' },
            { key: 'action', title: '动作' },
            { key: 'status', title: t('common.status'), render: statusRender },
            { key: 'attempts', title: '尝试次数' },
            { key: 'last_error', title: '错误', render: (r) => String(r.last_error ?? '-').slice(0, 30) },
          ]}
          fields={[
            { name: 'platform_id', label: '平台 ID (external-platforms)', required: true },
            { name: 'ref_type', label: '对象类型', type: 'select', options: [
              { value: 'product', label: '产品' }, { value: 'product_batch', label: '产品批次' },
              { value: 'trace_event', label: '溯源事件' }, { value: 'quality_report', label: '质检报告' },
            ] },
            { name: 'ref_id', label: '对象 ID', required: true },
            { name: 'action', label: '动作', type: 'select', initialValue: 'push', options: [
              { value: 'push', label: 'push' }, { value: 'verify', label: 'verify' },
            ] },
          ]}
        />
      )
    default:
      return <Navigate to="/" replace />
  }
}

export function SimpleRoutes() {
  return (
    <Routes>
      <Route path="/crops" element={<SimpleResource kind="crops" />} />
      <Route path="/varieties" element={<SimpleResource kind="varieties" />} />
      <Route path="/crop-batches" element={<SimpleResource kind="crop-batches" />} />
      <Route path="/inputs" element={<SimpleResource kind="inputs" />} />
      <Route path="/input-batches" element={<SimpleResource kind="input-batches" />} />
      <Route path="/input-usages" element={<SimpleResource kind="input-usages" />} />
      <Route path="/disease-records" element={<SimpleResource kind="disease-records" />} />
      <Route path="/device-models" element={<SimpleResource kind="device-models" />} />
      <Route path="/devices" element={<SimpleResource kind="devices" />} />
      <Route path="/geofences" element={<SimpleResource kind="geofences" />} />
      <Route path="/sensors" element={<SimpleResource kind="sensors" />} />
      <Route path="/datastreams" element={<SimpleResource kind="datastreams" />} />
      <Route path="/observations" element={<SimpleResource kind="observations" />} />
      <Route path="/alerts" element={<SimpleResource kind="alerts" />} />
      <Route path="/alert-rules" element={<SimpleResource kind="alert-rules" />} />
      <Route path="/harvests" element={<SimpleResource kind="harvests" />} />
      <Route path="/raw-material-batches" element={<SimpleResource kind="raw-material-batches" />} />
      <Route path="/processing-batches" element={<SimpleResource kind="processing-batches" />} />
      <Route path="/products" element={<SimpleResource kind="products" />} />
      <Route path="/product-batches" element={<SimpleResource kind="product-batches" />} />
      <Route path="/warehouses" element={<SimpleResource kind="warehouses" />} />
      <Route path="/inventory" element={<SimpleResource kind="inventory" />} />
      <Route path="/logistics" element={<SimpleResource kind="logistics" />} />
      <Route path="/quality-tests" element={<SimpleResource kind="quality-tests" />} />
      <Route path="/quality-reports" element={<SimpleResource kind="quality-reports" />} />
      <Route path="/evidence" element={<SimpleResource kind="evidence" />} />
      <Route path="/blockchain-proofs" element={<SimpleResource kind="blockchain-proofs" />} />
      <Route path="/external-platforms" element={<SimpleResource kind="external-platforms" />} />
      <Route path="/sync-tasks" element={<SimpleResource kind="sync-tasks" />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
