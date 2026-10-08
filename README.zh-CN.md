<div align="center">

# WhereToLive

### 下一个生活的地方，从可信信息开始。

面向长期居住、移居、留学和远程工作的开放信息平台。

[![CI](https://github.com/psmMRFP/WhereToLive/actions/workflows/ci.yml/badge.svg)](https://github.com/psmMRFP/WhereToLive/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE)
[![Stage](https://img.shields.io/badge/Stage-Early%20Development-orange.svg)](#当前进度)

[English](README.md) · **简体中文** · [Deutsch](README.de.md) · [Français](README.fr.md) · [Español](README.es.md)

[项目愿景](#为什么做-wheretolive) · [当前进度](#当前进度) · [本地开发](#本地开发) · [参与贡献](#参与贡献)

</div>

---

## 为什么做 WhereToLive

选择一个城市生活，需要的不只是旅游攻略。签证是否可行、税务规则如何、租房和日常开销多少，以及真正住过的人怎么看，应该能在同一个地方查到。

WhereToLive 希望把 **可信事实、居民体验和个人偏好** 放在一起，同时明确每条信息的来源、有效时间和更新历史。

> 这个地方客观上怎么样，与这个地方是否适合你，是两个不同的问题。

| 评价体系           | 回答的问题                         | 设计范围             |
| ------------------ | ---------------------------------- | -------------------- |
| **Data Score**     | 客观数据表现如何？                 | 0–100，展示各项维度  |
| **Resident Score** | 经过居住验证的用户怎么看？         | 1–10，使用贝叶斯收缩 |
| **Your Fit**       | 是否符合你的预算、语言和生活偏好？ | 按个人权重计算       |

三套评价保持独立，目前尚未实现。**WhereToLive Atlas** 是改造代号，正式产品名称为 **WhereToLive**。

## 当前进度

项目处于早期开发阶段。当前可运行的是公开地点目录和运营基础，尚无生产地点数据集。

| 已实现       | 功能                                                                 |
| ------------ | -------------------------------------------------------------------- |
| 公开网站     | 无需登录的地点搜索、分页与详情页面                                   |
| 多语言 UI    | English、简体中文、Deutsch、Français、Español                        |
| 地理模型     | 国家、地区、城市、城区、岛屿，以及稳定 slug 和多语言别名             |
| 地点运营     | 后台创建草稿、编辑、发布与撤回，版本冲突保护和事务审计               |
| API 契约     | Go 服务端与两个前端共享 OpenAPI，生成类型和查询客户端                |
| 候选池维护 | 按国家、覆盖等级和发布状态组合筛选，支持多语言别名搜索 |
| 地点测试数据 | 40 个手工地理草稿，仅供开发/E2E；支持预览、续跑和审计 |
| 反馈核心 | 私密人工录入、分类/状态筛选、处理结果、并发版本检查和事务审计 |
| 数据库初始化 | 缺失的专用数据库可自动创建，禁止使用默认 `postgres` 角色或数据库     |
| 消费者账户   | 注册、邮箱验证、登录、会话恢复及密码恢复代码；部署配置齐全前保持关闭 |

**接下来：** 覆盖与优先级规则、公共反馈及滥用控制 → 来源、证据和事实版本 → Research Agent → 签证、税务与生活成本 → 统一反馈 → 居住验证与评论 → 个人适配度。

评论翻译也在计划内：阅读语言与评论原文语言不同时提供 AI 翻译，用户可开启自动翻译，并始终保留查看原文的入口。

信息与评分维度计划由管理端维护，分别控制展示与是否参与评分；Web3／虚拟货币友好度、外汇与资本管制属于候选维度。这项配置能力尚未实现。

## 产品原则

- **证据优先：** AI 用于检索、提取、翻译和比较；AI 本身不是信息来源。
- **历史可查：** 时间敏感事实保留版本，展示来源、有效日期和最后核验时间。
- **体验与事实分开：** 官方数据、居民体验和当前警示明确区分。
- **隐私最小化：** 居住证明不公开，默认不发送给第三方大模型；审核后删除原文件，只保留必要验证信息。
- **商业独立：** 广告不能影响覆盖优先级、客观评分、居民评分或个人适配度。

以上是产品设计约束，相关业务功能正在逐步实现。

## 技术与目录

**Go 模块化单体 + PostgreSQL + React**。业务模块保持本地调用，第一阶段使用 PostgreSQL 搜索，不引入独立搜索集群或微服务体系。

```text
WhereToLive/
├── backend/     Go API、业务模块、SQL 查询与数据库迁移
├── web/         面向普通用户的公开 React 网站
├── admin/       运营与审核 React 后台
├── api/         唯一权威 HTTP 契约与代码生成配置
├── scripts/     契约、模块归属与源码边界检查
└── .github/     CI 工作流
```

前端使用 React、Vite、React Router、TanStack Query 和 Ant Design；数据库查询使用 sqlc，HTTP 类型与客户端由 OpenAPI 生成。

## 本地开发

### 1. 准备环境

版本要求以仓库配置为准：当前为 **Go 1.27、Node.js ≥ 26、npm ≥ 12**。完整验证还需已安装的 `oapi-codegen`、`sqlc`、`golangci-lint`、Python 3 和 Make；工具版本见 [CI 配置](.github/workflows/ci.yml)。PostgreSQL CI 使用版本 17。

从仓库根目录安装锁定的前端依赖：

```fish
npm ci --prefix admin
npm ci --prefix web
```

### 2. 配置后端

参考 [backend/.env.example](backend/.env.example)，通过进程环境或部署密钥机制提供配置。程序不会自动加载 `.env` 文件。

配置项：

| 环境变量                      | 用途                                                         |
| ----------------------------- | ------------------------------------------------------------ |
| `WHERETOLIVE_DATABASE_URL`         | 专用 PostgreSQL 角色和命名数据库的连接 URL；必需             |
| `WHERETOLIVE_AUTH_SIGNING_KEY`     | 至少 32 字节的签名密钥；必需                                 |
| `WHERETOLIVE_AUTH_COOKIE_SECURE`   | 本地 HTTP 开发可设为 `false`；生产环境保持 `true` 并使用 TLS |
| `WHERETOLIVE_DATABASE_AUTO_CREATE` | 默认 `true`；预先建库后可设为 `false`                        |

只有 PostgreSQL 明确返回“目标数据库不存在”时，系统才尝试建库。创建时通过 `template1` 连接，使用 `template0` 模板；专用角色需具有 `CREATEDB` 权限。**建库不等于迁移表结构。**

可单独初始化空数据库，无需配置认证签名密钥：

```fish
cd backend
go run ./cmd/wheretolive-db-init
```

建库后，使用兼容 `golang-migrate` 的工具按顺序应用 [数据库迁移](backend/internal/platform/database/migrations)。完成后，从 `backend/` 启动 API：

```fish
go run ./cmd/wheretolive
```

### 3. 启动前端

在仓库根目录的独立终端中运行：

```fish
npm run dev --prefix web
```

```fish
npm run dev --prefix admin
```

| 服务     | 本地地址                |
| -------- | ----------------------- |
| 公开网站 | `http://localhost:5174` |
| 运营后台 | `http://localhost:5173` |
| 后端 API | `http://localhost:8080` |

前端开发服务器将 `/api` 代理到后端。公开地点页面采用 `/{locale}/places/{slug}`，切换语言不会改变地点 slug。无已发布地点时，网站显示空目录；目前公开站保持 `noindex`。

## 验证

从仓库根目录运行：

```fish
make verify
```

包含代码生成一致性、OpenAPI 校验、Go 格式与静态检查、单元测试、两个前端的格式 / lint / 类型 / 组件测试 / 构建，以及数据库表归属和源码边界检查。

真实 PostgreSQL 集成测试需要设置 `WHERETOLIVE_TEST_DATABASE_URL`，目标必须是名称以 `_test` 结尾的专用测试库。测试会重置其 `wheretolive` schema，禁止指向业务数据库。

```fish
make backend-test-integration
```

公开网站浏览器检查使用已安装的 Chromium 和固定 API 测试数据：

```fish
make web-e2e
```

它验证页面交互，不替代真实数据库测试。后台 E2E 使用 `make admin-e2e`，要求数据库名为 `wheretolive_test`；完整发布检查为 `make verify-release`，另包含依赖漏洞和许可证检查。

## 参与贡献

先阅读 [AGENTS.md](AGENTS.md)，再检查现有模块和 [OpenAPI 契约](api/openapi.yaml)。HTTP 契约、实现、生成客户端和测试应一起更新；生成文件不能手动修改。

架构与产品工作文档目前保存在仓库外，仓库中的历史文档链接可能不可用。不要将本地密钥、环境配置、居住证明或用户私密数据提交到仓库。

欢迎通过 [Issues](https://github.com/psmMRFP/WhereToLive/issues) 反馈问题和建议，或提交 Pull Request。更新项目状态或开发说明时，请同步五种语言的 README。

## 许可证

采用 [GNU AGPL v3.0](LICENSE)（`AGPL-3.0-only`）。第三方依赖与数据来源保留各自的许可证。
