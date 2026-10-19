# Evals

Pacote `evals/`: roda um alvo (agent, chain ou modelo) sobre um dataset e pontua cada saída com avaliadores.
Tudo roda offline com `models.Fake` e `embeddings.Fake`. Exemplo: `go run ./examples/evals/basic`.

## API

```go
ds, _ := evals.LoadJSONL("qa.jsonl")          // uma evals.Example por linha: id, inputs, outputs, metadata
rep, err := evals.Evaluate(ctx, target, ds, []evals.Evaluator{
    evals.ExactMatch("answer"),                // outputs["answer"] == referência (strings sem espaços nas pontas)
    evals.Contains("answer"),                  // contém a referência
    evals.RegexMatch("answer", regexp.MustCompile(`\d+`)),
    evals.JSONEqual("data"),                   // mesmo valor JSON, ignora ordem de chaves
    evals.EmbeddingDistance(emb, "answer", 0.85), // cosseno >= limiar
    evals.LLMJudge(judgeModel, rubric),        // saída estruturada: score 0..1 + reasoning
    evals.TrajectoryMatch(evals.Subset, "tools"), // strict | unordered | subset | superset
}, evals.WithConcurrency(4), evals.WithTimeout(time.Minute))
evals.WriteText(os.Stdout, rep)                // tabela por exemplo + média por chave
evals.WriteJSON(os.Stdout, rep)
```

- `target` é `func(ctx, inputs) (outputs, error)`. Erro do alvo não interrompe a execução: os avaliadores recebem `Run.Err` e o erro vai para o relatório.
- `Evaluator` é uma interface com `Evaluate(ctx, Run, Example) (Result, error)`; `EvaluatorFunc` adapta uma função. Um avaliador com erro não gera nota (vai para `EvalErrors`).
- `Report.Summary` é a média de cada chave (`Result.Key`) sobre os exemplos que a produziram.
- Trajetória: o alvo põe a lista de nomes de tools em `outputs[campo]`; `evals.ToolNames(msgs)` extrai isso das mensagens.
- Ordem do relatório segue o dataset, mesmo com concorrência.

## Não portado

- Experimentos hospedados, filas de anotação e comparação de experimentos do LangSmith (`langsmith.evaluation` online). Só a parte offline foi portada.
- Envio dos resultados ao LangSmith: o cliente em `tracing/langsmith` só envia runs de tracing, não datasets nem experimentos. Seria um cliente novo para a API de experimentos.
