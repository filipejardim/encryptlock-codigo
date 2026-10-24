// Local test chain only: publishes a fake USDC and the swap vault and writes
// their addresses to local.json (for scripts/pay-local.js and the
// LockKeepers' [polygon] settings).
//   npx hardhat node                                   (in another window)
//   npx hardhat run scripts/setup-local.js --network localhost
const fs = require("fs");
const { ethers } = require("hardhat");

async function main() {
  const usdc = await (await ethers.getContractFactory("TestUSDC")).deploy();
  const vault = await (await ethers.getContractFactory("ERC20SwapTimestamp")).deploy();
  await vault.waitForDeployment();
  const out = { usdc: await usdc.getAddress(), vault: await vault.getAddress(), startBlock: await ethers.provider.getBlockNumber() };
  fs.writeFileSync(__dirname + "/../local.json", JSON.stringify(out, null, 2));
  console.log(JSON.stringify(out));
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
