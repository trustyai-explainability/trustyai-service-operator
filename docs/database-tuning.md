# TrustyAI Service database tuning

For `storage.format: DATABASE`, use `storage.databaseTuning` to import nonsecret
database tuning values from a ConfigMap. The ConfigMap must be in the same
namespace as the `TrustyAIService`. ConfigMap values are read when a pod starts;
after updating the ConfigMap, manually restart the TrustyAI Service Deployment
to load the new values. Restarting the operator is not necessary. For example:

```sh
oc rollout restart deployment/trustyai-service -n my-namespace
```

The tuning variables below are supported by the TrustyAI Service storage
implementation introduced in [trustyai-service PR #334](https://github.com/trustyai-explainability/trustyai-service/pull/334):

| Variable | Purpose |
| --- | --- |
| `DATABASE_POOL_SIZE` | SQLAlchemy pool size. |
| `DATABASE_MAX_OVERFLOW` | Maximum connections opened above the pool size. |
| `DATABASE_POOL_TIMEOUT` | Seconds to wait for a connection from the pool. |
| `DATABASE_POOL_RECYCLE` | Seconds before a pooled connection is recycled. |
| `DATABASE_MAX_CONNECTIONS_BUDGET` | Per-service-replica connection budget; pool size plus overflow must fit within it. |
| `DATABASE_APPLICATION_NAME` | PostgreSQL session application name. |
| `DATABASE_STATEMENT_TIMEOUT_MS` | PostgreSQL statement timeout in milliseconds. |
| `DATABASE_SYNCHRONOUS_COMMIT` | PostgreSQL synchronous commit setting, such as `on` or `off`. |
| `DATABASE_SQLITE_JOURNAL_MODE` | SQLite journal mode (`DELETE`, `TRUNCATE`, `PERSIST`, `MEMORY`, `WAL`, or `OFF`). |
| `DATABASE_SQLITE_SYNCHRONOUS` | SQLite synchronous setting (`OFF`, `NORMAL`, `FULL`, or `EXTRA`). |
| `DATABASE_SQLITE_BUSY_TIMEOUT_MS` | SQLite lock wait timeout in milliseconds. |
| `DATABASE_SQLITE_CACHE_SIZE_KB` | SQLite page cache size in KiB. |

The operator checks each value before the Deployment is updated. Numeric
variables must be non-negative integers, `DATABASE_APPLICATION_NAME` must not be
empty, and the mode variables must be one of the listed values. An invalid value
sets the `DatabaseTuningAvailable` condition to `False` with reason
`InvalidConfiguration`.

Create a ConfigMap with the tuning values. Values are strings, so quote numeric
and boolean-looking values:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: trustyai-db-tuning
  namespace: my-namespace
data:
  DATABASE_POOL_SIZE: "5"
  DATABASE_MAX_OVERFLOW: "10"
  DATABASE_POOL_TIMEOUT: "30"
  DATABASE_POOL_RECYCLE: "1800"
  DATABASE_MAX_CONNECTIONS_BUDGET: "15"
  DATABASE_APPLICATION_NAME: "trustyai-service"
  DATABASE_STATEMENT_TIMEOUT_MS: "30000"
  DATABASE_SYNCHRONOUS_COMMIT: "on"
```

Reference the ConfigMap from a `TrustyAIService` using database storage. Keep
the database connection and credentials in the existing Secret referenced by
`storage.databaseConfigurations`:

```yaml
apiVersion: trustyai.opendatahub.io/v1
kind: TrustyAIService
metadata:
  name: trustyai-service
  namespace: my-namespace
spec:
  storage:
    format: DATABASE
    databaseConfigurations: trustyai-db-credentials
    databaseTuning: trustyai-db-tuning
  metrics:
    schedule: "5s"
```

The referenced Secret must continue to provide `databaseKind`,
`databaseUsername`, `databasePassword`, `databaseService`, `databasePort`, and
`databaseName`. Keep credentials, database connection details, and TLS material
in their existing Secret-backed configuration; use the tuning ConfigMap only
for nonsecret tuning values.
