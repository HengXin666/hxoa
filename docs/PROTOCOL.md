# hxoa 协议 v2

所有 agent 请求带 `Authorization: Bearer <token>`。响应都是 JSON; 失败时为
`{"ok":false,"error":"...","code":"...","current_version":"..."}`。

| code | HTTP | 含义 |
|---|---|---|
| conflict | 409 | 读取后文件被改过; `current_version` 是现在的版本 |
| exists | 409 | `if_match:"new"` 但文件已存在 |
| no_match / ambiguous | 409 | replace 的 old 没找到 / 出现多次 |
| not_found | 404 | 路径不存在 |
| denied | 403 | 越出工作区, 写 .git/.hxoa, 或命令执行已关闭 |
| unauthorized | 401 | 令牌无效或已删除 |
| too_large | 413 | 大文件 (>512K) 没给行范围 |

版本是内容 sha256 的前 16 个十六进制字符。

## 端点

| 方法 路径 | 说明 |
|---|---|
| GET `/v1/hello` | 工作区, 身份, 模式, 根目录, 分支; 同时记一次在线 |
| POST `/v1/bye` | 客户端登出, 面板显示已断开 |
| GET `/v1/ls?path=&depth=&max=` | `{entries:[{p,d,b}]}`, 跳过 .git node_modules 的内部 |
| GET `/v1/read?path=&start=&end=` | `{version, lines, start, end, range_version, content}` |
| POST `/v1/write` | `{path, content, if_match}`; if_match = 版本 / `"new"` / `"*"` |
| POST `/v1/patch` | `{path, if_match \| range_match, edits:[...]}` 见下 |
| POST `/v1/rm` | `{path, if_match}` |
| POST `/v1/mv` | `{from, to, if_match}` |
| POST `/v1/grep` | `{pattern, path, glob, fixed, icase, max}` → `{text}` |
| POST `/v1/exec` | `{cmd, dir, timeout, bg, stdin}` → `{code, stdout, stderr, ms, timed_out}` 或 `{job}` |
| GET `/v1/job?id=&wait=&kill=1` | 后台任务状态和输出 |
| GET `/v1/tar?path=&all=1` | 目录的 tar.gz |

### patch

```json
{"path":"a.go","if_match":"<version>","edits":[
  {"op":"lines","start":10,"end":12,"lines":["新的第10行","..."]},
  {"op":"lines","start":30,"end":29,"lines":["插在第30行前"]}
]}
```

- `lines`: 行号都指读取时的文件, 多个编辑一次原子应用, 不能重叠。
- `replace`: `{"op":"replace","old":"...","new":"...","all":false}`, old 默认必须唯一。
- 一次 patch 不混用两种 op。
- `range_match`: 只配一个 `lines` 编辑, 校验的是那几行的 `range_version`;
  范围外的改动不影响, 范围内或行号移动都会冲突。

## 面板 API (只在 127.0.0.1:7071)

`GET /api/state`, `GET /api/ops`, `POST/PATCH/DELETE /api/workspaces[/{id}]`,
`POST /api/workspaces/{id}/tokens`, `GET /api/tokens/{id}/prompt`,
`DELETE /api/tokens/{id}`, `POST /api/tokens/{id}/merge`, `GET /api/browse?path=`。

在线 = 3 分钟内有请求且没有登出。删除令牌立即生效。
