# simulador-v2

Compara ofertas de crédito à habitação dos bancos portugueses, correndo os
simuladores públicos ao vivo, e publica a série temporal do preçário de mercado.

**Go · PostgreSQL · Redis · chi · pgx/sqlc · OpenAPI.** Serve **apenas JSON**: a
interface é uma app React Native, em repositório à parte.

> **Estado: em planeamento.** Ainda não há código. O desenho está fechado e o
> trabalho está nas issues. Ver [`PLAN.md`](PLAN.md).

Reescrita do [`simulador-credito-habitacao`](../simulador-credito-habitacao)
(Python/FastAPI, 10 bancos, a funcionar). O v1 continua a ser a referência
técnica sobre os bancos — o que se apurou sobre cada um está preservado em
[`docs/DOSSIE-BANCOS.md`](docs/DOSSIE-BANCOS.md).

## Documentos

| documento | o que decide |
|---|---|
| [`docs/ARQUITETURA.md`](docs/ARQUITETURA.md) | camadas, modelo de dados, cache, portões. **Vinculativo.** |
| [`docs/CONTRATO-BANCO.md`](docs/CONTRATO-BANCO.md) | como se acrescenta um banco, e a disciplina de captura primeiro |
| [`docs/DOSSIE-BANCOS.md`](docs/DOSSIE-BANCOS.md) | o que o v1 apurou sobre cada um dos 10 bancos |
| [`docs/API.md`](docs/API.md) | as duas fronteiras HTTP |
| [`docs/ECRAS.md`](docs/ECRAS.md) | os ecrãs da app |
| [`docs/APP.md`](docs/APP.md) | a stack da app e o que ela impõe ao backend |
| [`docs/USO-RESPONSAVEL.md`](docs/USO-RESPONSAVEL.md) | carga nos bancos, dados pessoais, e o que exige parecer |
| [`PLAN.md`](PLAN.md) | as fases e a ordem |
| [`RESUME.md`](RESUME.md) | estado da sessão e próximos passos |

## Bancos

Nenhum implementado ainda. A ordem está na [fase 1 do plano](PLAN.md#fase-1--a-fatia-vertical):
CGD, Novo Banco, Montepio e Banco CTT primeiro — todos de HTTP puro, sem browser.
Depois Santander e Crédito Agrícola. Por último os quatro que exigem browser
(ActivoBank, Millennium BCP, Bankinter, BPI).

## Fronteiras

- `GET /api/v1/*` — a app. Versionada, evolui connosco.
- `GET /api/rate-catalog` — o `viabilidade-imobiliaria`, autenticado por
  `X-API-Key`. ⚠️ **Congelada e compatível ao byte com o v1**: mudar o formato
  parte o outro repositório.

## Uso responsável

Corre simuladores **públicos** dos bancos, com frequência baixa e cache, e nunca
endpoints de registo de contactos. Os valores são **indicativos** — não são
propostas nem aconselhamento financeiro.
