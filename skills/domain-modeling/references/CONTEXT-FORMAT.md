# CONTEXT.md 格式

## 结构

```md
# {Context 名称}

{一两句话：这个 context 是什么、为什么存在。}

## 语言

**Order**:
{一两句话描述这个术语}
_Avoid_: Purchase, transaction

**Invoice**:
发货后向客户发出的付款请求。
_Avoid_: Bill, payment request

**Customer**:
下单的个人或组织。
_Avoid_: Client, buyer, account
```

## 规则

- **要有立场**。同一概念存在多个说法时，选最好的一个，其余列进 `_Avoid_`。
- **定义要收紧**。最多一两句。定义它**是什么**，不是它做什么。
- **只收本项目特有的术语**。通用编程概念（timeout、错误类型、工具模式）即使项目大量使用也不收。收录前自问：这是本 context 特有的概念，还是通用编程概念？只有前者属于这里。
- **自然成簇时用子标题分组**。所有术语同属一个内聚领域时，平铺即可。

## 单 context vs 多 context

**单 context（多数仓库）**：根目录一个 `CONTEXT.md`。

**多 context**：根目录 `CONTEXT-MAP.md` 列出各 context、位置及相互关系：

```md
# Context Map

## Contexts

- [Ordering](./src/ordering/CONTEXT.md) — 接收并跟踪客户订单
- [Billing](./src/billing/CONTEXT.md) — 生成发票并处理支付
- [Fulfillment](./src/fulfillment/CONTEXT.md) — 管理仓库拣货与发货

## Relationships

- **Ordering → Fulfillment**：Ordering 发出 `OrderPlaced` 事件；Fulfillment 消费它开始拣货
- **Fulfillment → Billing**：Fulfillment 发出 `ShipmentDispatched` 事件；Billing 消费它生成发票
- **Ordering ↔ Billing**：共享 `CustomerId` 与 `Money` 类型
```

按现状推断结构：

- 存在 `CONTEXT-MAP.md` → 读它找各 context
- 只有根 `CONTEXT.md` → 单 context
- 都没有 → 第一个术语敲定时懒创建根 `CONTEXT.md`

多 context 时，推断当前话题属于哪个 context；判断不了就问用户，不要猜。

## 多 context 下的 ADR 分层

多 context 仓库中 ADR 也分层：系统级决策（跨 context 集成、全局技术选型）记根目录 `docs/adr/`；单个 context 内部的决策记该 context 自己的 `src/<ctx>/docs/adr/`。写 ADR 的流程和模板走 `architecture-decision-record` skill，本条只管"记在哪一层"。
