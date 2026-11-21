# ADR-006 SaaS 多租户补全

- 状态：已接受（2026-11-22）
- 关联：specs/08（安全/多租户）、ADR-003/005

## 背景

平台的隔离架构自第一版即按多租户设计（40+ 业务表 `tenant_id NOT NULL` + 应用层强制过滤 +
JWT `tid`），但缺少 SaaS 运营闭环：租户只能由 seed 创建、无平台管理员、前端无租户上下文。
隔离审计（2026-11-22）另发现 6 处弱点。

## 决策

### 1. 平台管理员模型
- `users.is_platform_admin` 标志，归属系统租户 `code='platform'`（独立于业务租户）。
- JWT 增加 `pa` claim；`HasPerm` 对平台管理员恒真；`RequirePlatformAdmin` 中间件保护
  `/platform/*` 端点。平台管理员不进入任何业务租户的角色体系。
- 自举：启动时 `EnsurePlatformAdmin` 保证账号存在（密码 `CTC_PLATFORM_ADMIN_PASSWORD`，
  默认值打警告日志）。

### 2. 租户生命周期
- `ProvisionTenant`（事务）：租户 + 3 内置角色（含权限绑定）+ 管理员用户，一次调用完成。
- 两条开户路径：公开 `POST /auth/register`（`CTC_REGISTRATION_ENABLED` 开关，默认开）与
  平台端 `POST /platform/tenants`；注册成功直接返回 token 对。
- 租户停用语义：`tenants.status='suspended'` 在登录与 refresh 时拒绝（存量 access token
  最长 30 分钟后失效——可接受窗口，记入本文档）。平台系统租户自身不可停用/改名。

### 3. 登录多租户歧义
无 `tenant_code` 且同名用户存在于多个租户：先验证密码（防枚举），命中多个则返回
409 `MULTI_TENANT_AMBIGUOUS` + 公开的租户选项列表，前端弹出租户选择后带 code 重登。

### 4. 隔离加固清单（全部落地）
| # | 弱点（审计） | 修复 |
|---|---|---|
| 1 | 公开 `/public/trace-media/{fileID}` 未绑定溯源链，任意 evidence 文件可被枚举读取 | SQL 增加 EXISTS：evidence 的 (ref_type,ref_id) 必须存在 active trace_code |
| 2 | `PATCH /users/{id}` 的 setRoles 无租户校验（唯一跨租户写） | 先校验目标用户归属，DELETE/INSERT 均带租户条件 |
| 3 | `loadPerms` 不校验角色租户归属 | join 加 `roles.tenant_id = $2` |
| 4 | `POST /sync-tasks/run` 触发全租户任务 | `RunOnce(ctx, tenantID)`：用户触发限定本租户，后台 worker 才扫全量 |
| 5 | ~10 处创建接口仅 FK 校验（跨租户引用注入） | 新增 `pkg/tenantref` 白名单归属校验，覆盖 trace code/event、task、input usage、movement、mission、device、sync task、ai review、evidence |
| 6 | 登录歧义 | 见第 3 条 |

### 5. 前端
- 登录响应/`/auth/me` 携带 tenant 与 platform_admin；顶栏租户徽标。
- 登录页 409 弹出租户选择；新增注册表单（开关关闭时服务端 403）。
- `/tenant` 租户信息页（修复死链菜单）；平台管理员专属 `/platform/tenants` 管理页
  （列表/创建/停用启用/重置管理员密码）。

## 后果

- SaaS 运营闭环：自助开户 → 隔离使用 → 平台治理，全部端到端可验证（冒烟隔离段）。
- 已知折衷：停用租户的存量 access token 有最长 30 分钟窗口（后续可加每请求租户状态缓存校验）；
  JWT 仍为全局密钥（伪造风险由密钥管理承担，生产必须换强密钥）。
