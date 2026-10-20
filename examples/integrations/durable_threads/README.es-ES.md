# durable_threads

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Estado de conversación que sobrevive a los reinicios porque vive en un checkpointer (almacenamiento compatible con S3 cuando está configurado).

## Qué hace

- Un grafo contador incrementa `turns` en cada ejecución del thread `customer-42`.
- Con `S3_ENDPOINT` definido, el checkpointer es `s3.NewSaver` (AWS S3, MinIO, Tigris, R2...); sin él, un saver en memoria. Ejecútalo dos veces sobre el mismo bucket y la cuenta continúa donde se quedó.

## Cómo aplicarlo

```go
c := &s3.Client{Endpoint: ep, Credentials: bedrock.Credentials{AccessKeyID: key, SecretAccessKey: secret}}
saver := s3.NewSaver(c, bucket, "threads/")
app, _ := g.Compile(graph.WithCheckpointer(saver))
```

Cambia el saver por el store que encaje con tu stack: `sqlsaver.New` (SQLite/PostgreSQL), `redis.NewSaver`, `mongo.NewSaver`, `dynamodb.NewSaver`. El código del grafo no cambia.

## Cómo ejecutarlo

```bash
go run ./examples/integrations/durable_threads
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Levanta MinIO (ver INTEGRATIONS.md, "Testing against real services") y ejecuta:
   `S3_ENDPOINT=http://localhost:9000 S3_BUCKET=demo S3_KEY=... S3_SECRET=... go run ./examples/integrations/durable_threads`
2. Ejecútalo dos veces: la segunda ejecución debe imprimir `turns so far: 3` y `4`.
3. En producción, usa credenciales con acceso solo a ese bucket/prefijo, habilita versionado/cifrado del bucket y define una política de retención (`DeleteThread` para conversaciones antiguas).
4. Registra los tipos de estado personalizados con `checkpoint.Register`; los valores se guardan como JSON.
5. Elige el backend según el patrón de acceso: S3 para threads archivados y baratos, Redis o SQL para chat de baja latencia.

## Ver también

- [INTEGRATIONS.md](../../../docs/es-ES/INTEGRATIONS.md)
- [Manual, capítulo 8: Persistencia, human-in-the-loop y streaming](../../../docs/es-ES/manual/08-persistencia-hitl-streaming.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
