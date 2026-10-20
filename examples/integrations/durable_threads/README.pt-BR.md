# durable_threads

[English](README.md) | **Português** | [Español](README.es-ES.md)

Estado de conversa que sobrevive a reinícios porque vive num checkpointer (armazenamento compatível com S3 quando configurado).

## O que faz

- Um grafo contador incrementa `turns` a cada execução da thread `customer-42`.
- Com `S3_ENDPOINT` definido, o checkpointer é `s3.NewSaver` (AWS S3, MinIO, Tigris, R2...); sem ele, um saver em memória. Rode duas vezes no mesmo bucket e a contagem continua de onde parou.

## Como aplicar

```go
c := &s3.Client{Endpoint: ep, Credentials: bedrock.Credentials{AccessKeyID: key, SecretAccessKey: secret}}
saver := s3.NewSaver(c, bucket, "threads/")
app, _ := g.Compile(graph.WithCheckpointer(saver))
```

Troque o saver pelo store que se encaixa na sua stack: `sqlsaver.New` (SQLite/PostgreSQL), `redis.NewSaver`, `mongo.NewSaver`, `dynamodb.NewSaver`. O código do grafo não muda.

## Como rodar

```bash
go run ./examples/integrations/durable_threads
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Suba o MinIO (veja INTEGRATIONS.md, "Testing against real services") e rode:
   `S3_ENDPOINT=http://localhost:9000 S3_BUCKET=demo S3_KEY=... S3_SECRET=... go run ./examples/integrations/durable_threads`
2. Rode duas vezes: a segunda execução deve imprimir `turns so far: 3` e `4`.
3. Em produção, use credenciais com acesso apenas àquele bucket/prefixo, habilite versionamento/criptografia do bucket e defina uma política de retenção (`DeleteThread` para conversas antigas).
4. Registre tipos de estado customizados com `checkpoint.Register`; os valores são gravados como JSON.
5. Escolha o backend pelo padrão de acesso: S3 para threads arquivadas e baratas, Redis ou SQL para chat de baixa latência.

## Veja também

- [INTEGRATIONS.md](../../../docs/pt-BR/INTEGRATIONS.md)
- [Manual, capítulo 8: Persistência, human-in-the-loop e streaming](../../../docs/pt-BR/manual/08-persistencia-hitl-streaming.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
