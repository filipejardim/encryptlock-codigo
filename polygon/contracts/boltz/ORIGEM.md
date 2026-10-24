# Origem destes contratos

`ERC20Swap.sol`, `ERC20SwapTimestamp.sol` e `TransferHelper.sol` são copiados **sem alterações** do projeto
público Boltz (licença MIT, ver `LICENSE`):

- https://github.com/BoltzExchange/boltz-core
- commit `a932d49c4daaeae3d7940dc1519bf77ef92e6dc1` (24 jul 2026), contrato versão 6

É o "cofre" (HTLC) das trocas LOCK ↔ USDC (usamos `ERC20SwapTimestamp`: prazos em hora, não em número de bloco): o comprador prende os USDC com o hash
de um segredo; qualquer pessoa que mostre o segredo faz com que os USDC vão para
o vendedor; se ninguém o mostrar até ao prazo, os USDC voltam ao comprador.

Não encontrámos relatório público de auditoria deste contrato. É usado pela
Boltz com dinheiro real há anos. **Antes de dinheiro real no EncryptLock: decisão
do fundador sobre auditoria.**
