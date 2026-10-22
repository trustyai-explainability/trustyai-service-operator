# TrustyAI Service database tuning

For `storage.format: DATABASE`, use `storage.databaseTuning` to import nonsecret
database tuning values from a ConfigMap. The ConfigMap must be in the same
namespace as the `TrustyAIService`. The operator hashes its data into the pod
template, so changing a value rolls out pods with the updated environment.

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
```

The referenced Secret must continue to provide `databaseKind`,
`databaseUsername`, `databasePassword`, `databaseService`, `databasePort`, and
`databaseName`. Do not put credentials, connection details, or TLS material in
the tuning ConfigMap. The operator rejects ConfigMap keys that would override
the existing Secret-backed database connection environment variables.
