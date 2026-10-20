# permission_rag

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um retriever que só devolve documentos que quem consulta tem permissão de ler.

## O que faz

- Um servidor de políticas de mentira faz o papel do PDP do Permit.io: `alice` pode ler o manual e os salários, `bob` apenas o manual.
- `permit.FilterRetriever` envolve o retriever do vector store e consulta o motor de políticas sobre cada resultado, então Bob nunca vê o documento de salários. Com SpiceDB o código é o mesmo usando `integrations/spicedb.FilterRetriever`.

## Como aplicar

```go
inner := retrievers.FromVectorStore(vs, retrievers.VectorStoreOptions{K: 5})
r := permit.FilterRetriever(inner, &permit.Client{PDPURL: url, APIKey: key}, user, "read", "doc", idOf)
```

Filtre **antes** de o texto chegar ao modelo: uma vez que um trecho restrito está no prompt, o modelo pode vazá-lo. Coloque o id do documento nos metadados na hora de indexar para que `idOf` o encontre.

## Como rodar

```bash
go run ./examples/integrations/permission_rag
```

Este exemplo não chama modelo, então não precisa de chave de API e é determinístico.

## Como testar em modo produtivo

1. Aponte o cliente para o seu PDP real do Permit.io (ou SpiceDB) e modele os mesmos recursos e papéis que a sua aplicação usa.
2. Teste com uma matriz de usuários e documentos, incluindo um usuário sem permissões e um documento sem política (negar por padrão).
3. Busque a mais (`K` maior que o necessário) porque o filtro remove resultados, e depois reduza à contagem final.
4. Registre os ids dos documentos negados por requisição para auditoria, nunca o conteúdo.
5. Trate a chamada de política como uma dependência: defina um timeout e falhe fechado.

## Veja também

- [INTEGRATIONS.md](../../../docs/pt-BR/INTEGRATIONS.md)
- [Manual, capítulo 14: Produção, segurança e troubleshooting](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
