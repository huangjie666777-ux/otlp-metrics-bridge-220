# otlp_metrics220

纯后端 OTLP DELTA 到 Prometheus 累积指标转换服务。接收 OTLP/HTTP Protobuf，仅接受 DELTA 单调 `Sum` 与带显式桶的 DELTA `Histogram`，通过 `GET /metrics` 输出 Prometheus 0.0.4 文本格式。

## 配置

使用环境变量配置：

- `OTLP_HTTP_ADDR`：监听地址，默认 `:4318`，例如 `127.0.0.1:4318`。
- `PORT`：仅配置端口，值如 `18080`；若同时设置 `LISTEN_ADDR`，以后者为准。
- `LISTEN_ADDR`：完整监听地址，优先级高于 `PORT`。
- `OTLP_MAX_SERIES`：最大序列数，默认 `10000`，必须为正整数；超限整批拒绝。
- `OTLP_IDLE_EXPIRE_SECONDS`：空闲序列过期秒数，默认 `300`，`0` 表示不过期。

序列由资源 `service.instance.id`、作用域名称、指标名和字符串点属性共同标识。导出标签包含固定的 `instance` 和 `scope`；点属性禁止使用 `instance`、`scope`、`le`。指标名和属性键必须符合 Prometheus 传统 ASCII 命名规则。

## 构建与自测

```bash
go test ./...
go build -o otlp-metrics220 .
go run ./cmd/sample-client sample.pb
```

## 启动与 curl 演示

```bash
PORT=18080 ./otlp-metrics220
curl -sS -f -X POST http://127.0.0.1:18080/v1/metrics \
  -H 'Content-Type: application/x-protobuf' \
  --data-binary @sample.pb -o /dev/null
curl -sS http://127.0.0.1:18080/metrics
```

示例输出包含：

```text
# TYPE requests_total counter
requests_total{instance="demo-instance",scope="demo",route="/hello"} 7
# TYPE latency histogram
latency_bucket{instance="demo-instance",scope="demo",route="/hello",le="0.1"} 1
latency_bucket{instance="demo-instance",scope="demo",route="/hello",le="1"} 2
latency_bucket{instance="demo-instance",scope="demo",route="/hello",le="+Inf"} 3
latency_sum{instance="demo-instance",scope="demo",route="/hello"} 2
latency_count{instance="demo-instance",scope="demo",route="/hello"} 3
```

## 累积语义

- 首次收到序列时初始化累积值。
- 新点开始时间等于上一区间结束时间时累加。
- 新点开始时间晚于上一区间结束时间时视为断档并重置。
- 上一区间原样重传幂等，不重复累加且不延长生命周期。
- 同区间异内容、倒退、其他重叠区间返回 `400`。
- 直方图边界必须有限、严格递增；桶数必须为边界数加一，桶计数和必须等于 `count`，同序列边界必须保持不变。
- 每批请求先校验和暂存，提交时一次性生效；并发抓取只看到完整快照。
