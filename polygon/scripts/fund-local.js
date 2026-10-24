// Local test chain only: gives test wallets POL (for the network fee) and
// test USDC, as a faucet would on Amoy.
//   ADDRESSES=0x…,0x… npx hardhat run scripts/fund-local.js --network localhost
const fs = require("fs");
const { ethers } = require("hardhat");

async function main() {
  const { usdc: usdcAddr } = JSON.parse(fs.readFileSync(__dirname + "/../local.json"));
  const [bank] = await ethers.getSigners();
  const usdc = await ethers.getContractAt("TestUSDC", usdcAddr);
  for (const to of process.env.ADDRESSES.split(",").map((a) => a.trim()).filter(Boolean)) {
    await (await bank.sendTransaction({ to, value: ethers.parseEther("10") })).wait();
    await (await usdc.mint(to, 1000n * 1000000n)).wait();
    console.log(`${to}: ${ethers.formatEther(await ethers.provider.getBalance(to))} POL, ${Number(await usdc.balanceOf(to)) / 1e6} USDC`);
  }
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
