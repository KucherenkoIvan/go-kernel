# Infrastructure Layer (Adapters)

Port implementations and application entry points. Imports from all layers (`domain/`, `application/`, `shared/*`).

## Structure

```
adapters/
  pg/
    coupon_repository.go
    overview_reader.go
    event_publisher.go         # outbox publisher
  kafka/
    event_publisher.go         # direct kafka publisher
    event_handler.go           # kafka consumer
  grpc/
    feature_controller.go
  httpapi/
    webhook_handler.go
  cron/
    job_cron.go
  someintegration/             # integrations with external services
    client.go
    mapper.go
    config.go
```

Adapters are mostly split by the technology they relate to.

If it is an integration with an external service — by the name of that integration.

## Adapter kinds

- **Repository / Reader** — port implementations for PostgreSQL. Mapping between domain objects and DB rows.
- **Event Publisher** — sending domain events through an outbox or directly to Kafka.
- **Handlers/Controllers** — gRPC, HTTP/webhook, the transport layer; calls use-cases.
- **Handler/Service/Cron/Something** — any other custom adapter kind that correctly describes its function.

## Composition Root

`feature.go` — the single place where ports are bound to adapters through constructors. Use-cases are built here as well.

```go
// internal/features/coupon/feature.go
package coupon

type Feature struct {
	// use-cases exposed to transports and cmd/
	UseCoupon         *usecoupon.UseCouponCommand
	GetCouponOverview *usecoupon.GetCouponOverviewQuery
	CouponUsedPolicy  *usecoupon.CouponUsedNotificationPolicy
	OverviewProjector *couponstats.CouponOverviewProjector
}

func New(db *postgres.Client, outbox *outbox.Service, perms *grpcperms.Client) *Feature {
	// port -> adapter
	var couponRepo ports.CouponRepository = pg.NewCouponRepository(db)
	var overviewReader ports.CouponOverviewReader = pg.NewOverviewReader(db)
	var events ports.CouponEventProducer = pg.NewEventPublisherOutbox(outbox, db)
	var permission ports.PermissionGateway = grpcadapter.NewPermissionGateway(perms)
	var txManager ddd.TxManager = db // the DB client implements TxManager

	// use-cases
	return &Feature{
		UseCoupon:         usecoupon.NewUseCouponCommand(txManager, couponRepo, events, permission),
		GetCouponOverview: usecoupon.NewGetCouponOverviewQuery(overviewReader),
		CouponUsedPolicy:  usecoupon.NewCouponUsedNotificationPolicy( /* ... */ ),
		OverviewProjector: couponstats.NewCouponOverviewProjector(overviewReader),
	}
}
```

There is no business logic here — only dependency assembly.

Swapping an implementation is a one-line change in the composition root:

```go
var events ports.CouponEventProducer = pg.NewEventPublisherOutbox(outbox, db) // via outbox
var events ports.CouponEventProducer = kafka.NewEventPublisher(producer)     // direct
```

---

Next: the [package guides](../../README.md#docs) — how each building block is used in practice.
