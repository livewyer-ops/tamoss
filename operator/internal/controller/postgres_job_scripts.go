package controller

import "strings"

func postgresWaitForReadyScript() string {
	return `export PGPASSWORD="${POSTGRES_PASSWORD}"
for i in $(seq 1 60); do
  if pg_isready -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" >/dev/null 2>&1; then
    break
  fi
  if [ "$i" = "60" ]; then
    pg_isready -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d "${POSTGRES_DB}"
    exit 1
  fi
  sleep 2
done`
}

func postgresPSQLScript(args []string, sql string) string {
	var builder strings.Builder
	builder.WriteString(postgresWaitForReadyScript())
	builder.WriteString("\n")
	builder.WriteString(`psql "host=${POSTGRES_HOST} port=${POSTGRES_PORT} dbname=${POSTGRES_DB} user=${POSTGRES_USER}" \`)
	builder.WriteString("\n")
	builder.WriteString("  -v ON_ERROR_STOP=1")
	if len(args) == 0 {
		builder.WriteString(" <<'SQL'\n")
	} else {
		builder.WriteString(" \\\n")
		for i, arg := range args {
			builder.WriteString("  ")
			builder.WriteString(arg)
			if i == len(args)-1 {
				builder.WriteString(" <<'SQL'\n")
			} else {
				builder.WriteString(" \\\n")
			}
		}
	}
	builder.WriteString(strings.TrimSpace(sql))
	builder.WriteString("\nSQL")
	return builder.String()
}

func postgresTransactionalSQL(sql string) string {
	return "BEGIN;\n" + strings.TrimSpace(sql) + "\nCOMMIT;"
}
