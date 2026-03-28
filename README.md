# emby-proxy

Go 实现的 Emby 反向代理，零配置自动改写响应中的 URL，运行于 Docker，前面套 Nginx Proxy Manager 即可使用。

## 原理

通过 URL 路径编码目标地址，自动反代任意 Emby 服务器：

```
https://你的域名/{scheme}/{domain}/{port}/{path}
```

程序自动完成：
- 响应体中所有绝对 URL 改写为代理地址（替代 nginx `sub_filter`）
- 302 重定向 Location 头改写（替代 nginx `proxy_redirect`）
- Referer/Origin 还原为真实上游地址（绕过防盗链）
- 请求/响应中 hop-by-hop 头正确处理，不跨连接转发
- 请求特征头抹除（X-Real-IP / X-Forwarded-* / Via）
- 响应特征头抹除（Server / X-Powered-By）
- Host 伪装 + TLS SNI 自动匹配
- 视频流式转发，支持 Range 断点续传
- WebSocket 透传（适配 Nginx Proxy Manager 的 WebSocket Support）

## 部署

```bash
docker-compose up -d --build
```

服务监听 `8080` 端口，在 Nginx Proxy Manager 中将你的域名反代到 `http://localhost:8080` 即可。

## Nginx Proxy Manager 推荐配置

新建 Proxy Host 后，转发到：

- **Scheme**: `http`
- **Forward Hostname / IP**: 运行本项目的 Docker 主机地址
- **Forward Port**: `8080`

推荐按钮状态：

- **WebSocket Support**: 开
- **Cache Assets**: 关
- **Block Common Exploits**: 建议先关，确认代理链稳定后再自行评估是否开启

`Custom Nginx Configuration` 建议只填写下面这三行：

```nginx
proxy_buffering off;
proxy_request_buffering off;
proxy_max_temp_file_size 0;
```

说明：

- 这三行的作用是减少 Nginx Proxy Manager 在中间层对请求和响应的额外缓冲，避免出现“上游下载很快、客户端实际接收很慢”时的过量预读与浪费。
- `WebSocket Support` 已经由 NPM 按自己的方式处理升级头，通常**不要**再在 `Custom Nginx Configuration` 里额外手写 `proxy_set_header Connection ...` 或 `proxy_set_header Upgrade ...`，否则可能导致配置冲突或网络错误。
- 如果你尝试加入 `proxy_http_version 1.1;` 后出现网络错误，说明当前 NPM 的配置注入位置不适合额外覆写这条指令。此时保留上面三行即可。
- 一般不需要在这里自己再包一层 `location / { ... }`；NPM 通常会把这段内容插入它已生成的 location 或 server 配置中。直接写指令即可。

## 使用示例

假设你的代理域名为 `https://proxy.example.com`：

| 场景 | 访问地址 |
|------|---------|
| HTTPS 默认端口 | `https://proxy.example.com/https/emby.example.com/443/` |
| HTTP 自定义端口 | `https://proxy.example.com/http/emby.example.com/2052/` |
| 前后端分离（主站） | `https://proxy.example.com/https/emby.example.com/443/` |

推流地址会被自动改写——客户端无需关心推流域名，所有流量都经过代理。

## 日志

```
[API]    200 GET emby.example.com/emby/Items/123 (45ms)
[STREAM] 206 GET cdn.example.com/videos/xxx.mp4 | 1.2 GB | 3m22s
[PROXY]  200 GET emby.example.com/web/index.html | 156 KB | 89ms
[WS]     101 GET emby.example.com/socket | up 12 KB | down 48 KB | 2m10s
[ERROR]  GET cdn.example.com/videos/fail.mp4 : connection refused
```

- `[STREAM]` 推流/视频，带传输量和耗时
- `[API]` JSON/HTML 等需要改写 body 的请求
- `[PROXY]` 其他二进制透传
- `[WS]` WebSocket 连接，带上下行字节数和耗时
- `[ERROR]` 上游连接失败或代理异常

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LISTEN_ADDR` | `:8080` | 监听地址 |

## 项目结构

```
├── main.go              # 入口
├── handler.go           # HTTP 主流程编排与响应处理
├── headers.go           # 请求/响应头规则与辅助函数
├── target.go            # 代理路径解析与 target 语义
├── websocket.go         # WebSocket 透传逻辑
├── rewriter.go          # 响应改写：URL 替换、Header 改写
├── Dockerfile           # 多阶段构建
└── docker-compose.yml
```
