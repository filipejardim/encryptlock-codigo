// Publishes the swap vault (Boltz ERC20SwapTimestamp) and prints its address
// and the block it was published in (where the LockKeepers start watching).
//   Local test:  npx hardhat run scripts/deploy.js
//   Amoy (test): AMOY_KEY=<key with test POL> npx hardhat run scripts/deploy.js --network amoy
//   Real Polygon: scripts\cofre-polygon.ps1 (POLYGON_KEY, --network polygon)
const { ethers, network } = require("hardhat");

async function main() {
  const vault = await (await ethers.getContractFactory("ERC20SwapTimestamp")).deploy();
  // On a real network, a few blocks on top so the address is final.
  const local = network.name === "hardhat" || network.name === "localhost";
  const receipt = await vault.deploymentTransaction().wait(local ? 1 : 3);
  console.log(`network: ${network.name}`);
  console.log(`vault:   ${await vault.getAddress()}`);
  console.log(`block:   ${receipt.blockNumber}`);
  console.log(`gas:     ${receipt.gasUsed}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
