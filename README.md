# hxoa

把你本机的工作区安全地交给远端 AI (比如 Hark) 读写和执行命令, 就像它坐在你电脑前一样。

```
 远端 AI ── hxoa (客户端) ──HTTPS──> Cloudflare 隧道 ──> hxoad (你本机) ──> 你的目录
                                                          └─ 面板 http://127.0.0.1:7071 (只在本机)
```

三个部分, 一个仓库:

| 部分 | 位置 | 作用 |
|---|---|---|
| `hxoad` 后端 | `cmd/hxoad`, `internal/server` | 跑在你本机; 管工作区, 令牌, 版本校验, 命令执行, workers |
| 面板 前端 | `web/ui` (React + Tailwind + Radix, 构建到 `web/static` 编译进 hxoad) | 选目录, 开关 workers / 命令执行, 发令牌, 看谁在线, 合并 |
| `hxoa` 客户端 | `cmd/hxoa` | AI 那边用的命令行, 输出尽量省 token |

## 部署

### Docker Compose (推荐)

```bash
git clone https://github.com/HengXin666/hxoa.git && cd hxoa
./scripts/setup.sh            # 生成 .env: 你的 uid/gid 和可分享的根目录 (默认 $HOME)
docker compose up -d --build
```

打开 http://127.0.0.1:7071 :

1. 添加工作区: 选一个目录, 决定要不要开 **workers** 和 **允许执行命令**。
2. 新建连接: 填一个身份名 (如 `hark`), 复制弹出的连接提示, 原样发给 AI。
3. 面板上能看到每个连接是否在线, 最后访问时间, 操作数, 最近操作。删除连接后它立刻失去访问权, 在线数同步减少。
4. **分工**: 每个连接在做哪些 issue。AI 用 `hxoa task 104` 登记, 你也可以在「连接」里给它派活; worker 分支上还没合并、也没推到远端的提交说明里写的 `#号` 也会算上。「分工」页按 issue 列出谁在做, 同一个 issue 落到两个身份会标红; origin 在 GitHub 时显示 issue 标题并能点开。 侧边栏底部「全局分工」把所有工作区放在一页, 随面板几秒刷新一次。

默认用免账号的 quick tunnel, 重启后地址会变 (面板和连接提示会自动用新地址)。要固定域名见 `docker-compose.named.yml`。

> Docker 模式下, AI 执行的命令跑在容器里 (镜像自带 git, gcc, python, node 等)。
> 如果需要你本机的完整工具链和环境, 用下面的原生方式。

### 原生运行

```bash
./scripts/run-native.sh       # 需要 Go 1.22+ 和 cloudflared
```

## 两种模式

**direct (workers 关)**: AI 直接改你的目录, 改动马上能看到。适合只有一个 AI 在改, 或者你不熟悉 git。

**worker (workers 开)**: 每个连接在 `.hxoa/workers/<身份>-<令牌id>` 的独立 git 工作树和分支 `hxoa/<身份>-<令牌id>` 里干活, 你的目录和其他 AI 都不受影响; 需求做完由 AI 自己合入最新主线, 解决冲突, 验证后推到远端主线 (没有远端时用 `hxoa merge`), 不用你操作; 你也可以在面板点 **合并** 把它的改动合进你当前的分支 (有冲突会自动撤销, 不会弄乱你的目录)。合并后分支会快进到你的分支, 它接着干活; 删除令牌时, 已全部合并的分支和干净的工作树会一起删掉, 没合并的留着。适合你和本地 Agent, 或多个 AI 并行。

两种模式下写入都有版本校验, 见下。

## 防止编辑冲突

- 每次读取都返回文件的 `version` (内容哈希); 按行读取还会返回这几行的 `range_version`。
- 每次写入都必须带上版本: 整文件写和 `replace` 用 `version`, 按行改可以只用 `range_version`。
- 从读取到写入之间, 只要文件 (或那几行) 被任何人改过, 包括你本地的编辑器和别的 Agent, 写入就返回 `conflict` (HTTP 409, 客户端退出码 3), 什么都不会写。
- 服务端对同一文件的 "检查 + 写入" 加锁, 写入是临时文件 + rename, 不会写出半个文件。
- 新建文件用 `--new`, 目标已存在会被拒绝, 不会误覆盖。
- 不允许 `..` 和符号链接越出工作区, 不允许直接写 `.git/` 和 `.hxoa/`。

## 客户端

```bash
hxoa login <token> --base <url>
hxoa guide                     # 完整用法和规则
hxoa ls src -d 2
hxoa grep 'func main' -g '*.go'
hxoa cat src/main.go 40 80 -n  # stderr: # src/main.go v=… lines 40-80/312 rv=…
hxoa replace src/main.go -v <v> --old '旧' --new '新'
hxoa patch src/main.go 40 42 -r <rv> < new_lines.txt
hxoa exec 'go test ./...'      # --timeout, --bg, hxoa job <id> --wait 60
hxoa pull src ./local-src      # 整个目录打包下载
```

协议细节见 [docs/PROTOCOL.md](docs/PROTOCOL.md)。

## 安全

- 令牌就是身份, 每个连接一个, 随时在面板删除; 删除工作区会吊销它的所有令牌。
- 面板 (7071) 默认只绑 127.0.0.1, 从不经过隧道; 隧道只暴露 agent API (7070)。
- 面板登录见下面「面板账号」。
- 可以按工作区关闭命令执行, 只保留文件读写。
- 状态保存在 `data/state.json` (权限 0600)。


## 远程管理

面板 "远程管理" 里可以建管理令牌 (默认没有, 即关闭), 让别的 App (如 HX-AIOA) 经公网地址 `/v1/admin/*` 管理工作区, 连接令牌, 选目录和合并, 并一键拿到连接提示。等于这台机器的远程控制权, 只交给信任的 App; 不能远程建管理令牌或改隧道。接口见 [docs/PROTOCOL.md](docs/PROTOCOL.md)。

## 面板账号

面板有自己的登录页, 右上角菜单里可以改用户名和密码 (保存后其它地方的登录全部失效)。账号存在数据目录的 `auth.json` (PBKDF2 哈希), 会话是签名 cookie, 7 天有效, 重启不掉线; 连续输错 5 次会锁一会儿。

| 配置 | 作用 |
|---|---|
| `HXOA_ADMIN_PASSWORD` / `-admin-password` | 初始密码, 用户名 `admin`。在面板里保存过账号后就不再用它 |
| `HXOA_PANEL_AUTH=password` (默认) | 面板自己登录 |
| `HXOA_PANEL_AUTH=none` / `-panel-auth none` | 面板不做登录, 交给前面的反向代理 (如雷池「身份认证」、SSO 网关)。只在面板端口只能经由它访问时使用 |

本机使用且没设密码时, 面板直接打开, 可以在菜单里设置账号。面板监听非本机地址时必须有账号, 或者用 `none` 模式。

把面板和 agent API 放在同一个域名下 (例如 VPS 上用雷池/nginx 反代): `/v1/` 转给 7070, 其余转给 7071, 并设 `HXOA_PUBLIC_URL=https://你的域名`, 连接提示就会带这个地址。用 `none` 模式交给雷池认证时, 记得让 `/v1/` 不走雷池的身份认证, 否则 hxoa 客户端连不上 (它用令牌认证)。

改前端: `cd web/ui && npm install && npm run dev` (把 `/api` 代理到本机 7071), 改完 `npm run build` 并提交 `web/static`。
