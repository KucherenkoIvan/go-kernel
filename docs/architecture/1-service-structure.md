# Backend Service Structure

All backend services follow a single top-level structure:

```
cmd/
  <app>/
    main.go                # entry point (application composition root)

internal/
  features/<feature>/      # feature modules (a group of related business scenarios)
    feature.go             # module composition root
    domain/                # pure business logic
    application/           # use-cases and ports
    adapters/              # port implementations (pg, kafka, grpc, http)

  shared/                  # reused across features
    domain/                # reusable VOs and events within the service
    ports/                 # abstractions for communication between features
    infra/                 # reusable infra packages (db, config, health, auth)
```


## cmd/

The application entry point. Contains the composition root — the `main` function that assembles all feature modules and shared infrastructure.

```
cmd/
  <app>/
    main.go               # bootstrap: builds infra, wires features, starts transports (gRPC, HTTP)
```

`main.go` is the only place where all modules of the service are assembled and where ports are wired to their implementations:

```go
func main() {
	// shared infra
	cfg := config.MustLoad()
	db := postgres.MustConnect(cfg.DB)

	// features + wiring of cross-feature ports
	usersFeature := users.New(db)
	billingFeature := billing.New(db, usersFeature.UserPort)

	// transports
	srv := grpcserver.New(usersFeature, billingFeature)
	srv.Run()
}
```

Cross-feature wiring happens right here: the owning feature exposes a port implementation, `main.go` passes it into the consuming feature's constructor. The consumer depends only on the interface from `shared/ports/`.

There is no business logic here — only assembly of modules and wiring of ports.


## features/

Each feature module is an isolated bounded context with its own domain logic.
Conceptually it is close to a `bounded context` from DDD.

Each feature:
- **Is self-sufficient** — contains everything from domain to adapters
- **Is isolated** — does not import other features directly
- **Has a single assembly point** — `feature.go`, where ports are bound to adapters via constructors


### Module structure

By default we use an approach combining DDD and hexagonal architecture.

```
internal/features/<feature>/
  feature.go                     # composition root (wiring: port -> adapter)

  domain/                        # pure business logic (no frameworks, no infra dependencies)
    coupon_aggregate.go          # aggregates — root objects with business logic, expressed in real-world terms
    line_item_entity.go          # entities — child objects of aggregates
    discount_vo.go               # a structure with business meaning + related functions
    events.go                    # business events
    read_models.go               # data structures for queries
    errors.go                    # business errors

  application/                   # orchestration of business scenarios
    ports/                       # abstract interfaces of infrastructure dependencies
      coupon_repository.go       # write/read aggregates
      overview_reader.go         # read read-models from the DB
      provider_gateway.go        # external service
    usecases/<usecase>/          # a set of commands and queries implementing a business scenario
      action_command.go          # command (state change, works with the aggregate)
      data_query.go              # query (returns a read-model)
      reaction_policy.go         # async event handling: business logic -> command call
      projection_projector.go    # async event handling: write into a reader

  adapters/                      # port implementations
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
    epingrid/
      epingrid_gateway.go
```

> **Note on packages:** unlike the original layout, `domain/` is a single Go package with files grouped by kind (`*_aggregate.go`, `*_vo.go`, `events.go`, …), not sub-packages. Domain types reference each other freely, and splitting them into sub-packages would create import cycles. `application/ports` and `application/usecases/*` are separate packages — their dependency direction is acyclic by design.

### Dependency direction

```
adapters  ->  application  ->  domain
   |              |              |
shared/infra  shared/ports  shared/domain
```

### Feature layers

- **[Domain](./2-domain-layer.md)** — pure business logic with no dependencies on frameworks or infrastructure. Aggregate, Value Object, Domain Events, Read Model, Domain Errors.

- **[Application](./3-application-layer.md)** — orchestration of business scenarios. Ports (infrastructure abstractions) and use-cases (Command, Query, Policy, Projector).

- **[Infrastructure](./4-infrastructure-layer.md)** — port implementations and entry points. PostgreSQL adapters, Kafka, gRPC handlers, Composition Root.

### Exceptions

For some services with minimal application/domain logic, a standard layered style may be used instead:

`handler -> service -> repository (standard layered approach)`

## shared/

Code reused across features. Split into three layers with different access levels.

### shared/domain/

Domain types shared by several features within one service: mostly Value Objects and domain events.

If the features are fully independent — this package may be omitted.

```
internal/
  shared/
    domain/
      email.go               # shared VO (defined type + constructor)
      item_purchased.go      # an event that several features subscribe to
```

If a type is used by only one feature — it stays in `features/<feature>/domain/`.

### shared/ports/

Abstractions for interaction between features. Each port is an interface.

```
internal/
  shared/
    ports/
      user_port.go             # abstraction: billing → users
      notification_port.go     # abstraction: orders → notifications
```

The owning feature implements the port in its adapters and exposes the implementation. Binding the abstraction to the implementation happens in `cmd/<app>/main.go` — the implementation is passed into the consuming feature's constructor. The consumer injects the port without knowing about the implementation.

### shared/infra/

Infrastructure packages — wrappers over external libraries and system services. Reused by all features through constructor parameters.

```
internal/
  shared/
    infra/
      config/
        config.go              # configuration loading and validation
      postgres/
        client.go              # DB client + TxManager implementation
      health/
        health.go              # health-check endpoint for k8s
      auth/
        auth.go                # JWT, middleware, session
```


## Dependency direction rule

```
cmd  →  features  →  shared
```

- `cmd/` may import from `features/` and `shared/`
- `features/` may import from `shared/`, but **NOT from other features**
- `shared/` imports neither from `cmd/` nor from `features/`

**Features do not import each other directly.** Interaction between features goes through abstractions in `shared/ports/`.

> **Enforcement:** the Go compiler rejects import *cycles* for free, but it will not stop a straight-line import of one feature from another — add an import linter (`go-arch-lint` or `depguard`) with these rules to CI.

## How features interact with shared

So that business logic is not tied to infrastructure, there are also restrictions on using `shared/infra` from `feature/domain` and `feature/application`:

| Layer | shared/domain | shared/ports | shared/infra |
|------|:-------------:|:------------:|:------------:|
| **feature/domain** | yes | no | no |
| **feature/application** | yes | yes | no |
| **feature/adapters** | yes | yes | yes |
| **cmd** | yes | yes | yes |

- `shared/domain/` — domain types reused across features within the service (VOs, events). Accessible from domain and above
- `shared/ports/` — abstractions for communication between features. Accessible from application and above.
- `shared/infra/` — infrastructure packages (db, config, auth, health). Accessible **only** from adapters and cmd.
