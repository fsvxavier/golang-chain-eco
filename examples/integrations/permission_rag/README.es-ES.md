# permission_rag

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un retriever que solo devuelve documentos que quien consulta tiene permiso de leer.

## Qué hace

- Un servidor de políticas de mentira hace el papel del PDP de Permit.io: `alice` puede leer el manual y los salarios, `bob` solo el manual.
- `permit.FilterRetriever` envuelve el retriever del vector store y consulta al motor de políticas sobre cada resultado, así que Bob nunca ve el documento de salarios. Con SpiceDB el código es el mismo usando `integrations/spicedb.FilterRetriever`.

## Cómo aplicarlo

```go
inner := retrievers.FromVectorStore(vs, retrievers.VectorStoreOptions{K: 5})
r := permit.FilterRetriever(inner, &permit.Client{PDPURL: url, APIKey: key}, user, "read", "doc", idOf)
```

Filtra **antes** de que el texto llegue al modelo: una vez que un pasaje restringido está en el prompt, el modelo puede filtrarlo. Pon el id del documento en los metadatos al indexar para que `idOf` lo encuentre.

## Cómo ejecutarlo

```bash
go run ./examples/integrations/permission_rag
```

Este ejemplo no llama a ningún modelo, así que no necesita clave de API y es determinista.

## Cómo probarlo en modo productivo

1. Apunta el cliente a tu PDP real de Permit.io (o SpiceDB) y modela los mismos recursos y roles que usa tu aplicación.
2. Prueba con una matriz de usuarios y documentos, incluido un usuario sin permisos y un documento sin política (denegar por defecto).
3. Trae de más (`K` mayor que el necesario) porque el filtro elimina resultados, y luego recorta al conteo final.
4. Registra los ids de los documentos denegados por petición para auditoría, nunca el contenido.
5. Trata la llamada de política como una dependencia: define un timeout y falla cerrado.

## Ver también

- [INTEGRATIONS.md](../../../docs/es-ES/INTEGRATIONS.md)
- [Manual, capítulo 14: Producción, seguridad y troubleshooting](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
