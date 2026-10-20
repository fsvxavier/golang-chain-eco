# vector_indexes

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Los dos índices vectoriales en proceso, exacto (flat) y aproximado (bosque estilo Annoy), con el recall medido contra la respuesta exacta.

## Qué hace

- Monta 5.000 vectores aleatorios de 32 dimensiones en `vectorstore.NewFlat()` y `vectorstore.Annoy{Trees: 20}`.
- Mide el recall@10 del bosque contra el índice exacto en el `SearchK` por defecto y en 1500 (más candidatos, mejor recall, consultas más lentas), luego guarda y recarga el índice flat y comprueba que vuelve el mismo vecino más cercano.

## Cómo aplicarlo

```go
flat := vectorstore.NewFlat()          // exact, simplest
ann  := &vectorstore.Annoy{Trees: 20}  // approximate, faster on large sets
flat.Upsert(ctx, records); hits, _ := flat.Query(ctx, vec, 10)
flat.Save(path); flat, _ = vectorstore.LoadFlat(path)
```

Empieza con `Flat`; pasa al bosque cuando la búsqueda exacta se vuelva demasiado lenta. Mide siempre el recall con **tus** vectores: los vectores gaussianos aleatorios, como aquí, son un caso difícil.

## Cómo ejecutarlo

```bash
go run ./examples/integrations/vector_indexes
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Usa tus embeddings reales y una muestra representativa de consultas, y elige `Trees` y `SearchK` para el recall que necesitas (por ejemplo 95%) con una latencia aceptable.
2. Reconstruye o recarga el índice al arrancar desde un archivo persistido; mantenlo consistente con la fuente de la verdad.
3. Para corpus muy grandes o compartidos, usa un store en red de `integrations/vectorstore` en lugar de un índice en proceso.
4. Vuelve a medir el recall tras cambiar el modelo de embeddings.

## Ver también

- [INTEGRATIONS.md](../../../docs/es-ES/INTEGRATIONS.md)
- [Manual, capítulo 6: RAG: loaders, splitters, embeddings, vector stores y retrievers](../../../docs/es-ES/manual/06-rag.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
