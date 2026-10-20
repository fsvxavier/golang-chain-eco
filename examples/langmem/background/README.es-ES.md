# background

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Memorias formadas "subconscientemente" cuando la conversación se calla: un extractor con debounce corre una vez sobre toda la conversación.

## Qué hace

- `ReflectionExecutor` envuelve un `StoreManager`. Cada turno llama a `Submit` con 300 ms de retraso en el mismo thread; cada submit cancela la ejecución pendiente y reprograma.
- Tres turnos rápidos disparan **una** extracción sobre toda la conversación, escribiendo tres memorias. Luego las busca de forma semántica.

## Cómo aplicarlo

```go
manager := langmem.NewStoreManager(model, langmem.Namespace{"memories", "{langgraph_user_id}"}, langmem.WithStore(st))
executor := &langmem.ReflectionExecutor{Manager: manager}
fut, _ := executor.Submit(ctx, langmem.Payload{Messages: convo}, langmem.SubmitOptions{After: 30 * time.Second})
defer executor.Shutdown(true, false) // flush pending work on exit
```

Úsalo para que la formación de memoria nunca retrase una respuesta. Elige el retraso según cuánto tiempo se queda quieta una conversación antes de "terminar" (de segundos a minutos).

## Cómo ejecutarlo

```bash
go run ./examples/langmem/background
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un modelo de extracción real y un store duradero; elige `After` según el ritmo real de tus conversaciones.
2. El timer es en proceso: al apagar llama a `Shutdown(true, false)` para vaciar, y acepta que una caída pierde extracciones pendientes (es un debounce, no una cola). Para un executor remoto, mira `remote/server` y la brecha señalada en LANGMEM.md.
3. Haz una comprobación de coste de la extracción: una extracción sobre toda la conversación por período de silencio, no por turno.
4. Revisa la calidad de las memorias extraídas en conversaciones reales antes de habilitarlo para todos los usuarios.

## Ver también

- [Manual, capítulo 16: Memoria a largo plazo (LangMem)](../../../docs/es-ES/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/es-ES/LANGMEM.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
