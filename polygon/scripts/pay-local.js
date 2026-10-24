// Local test chain only: pays a trade the way the buyer's app will. The buyer
// locks the USDC in the vault for the seller with the secret's hash, then
// shows the secret: the USDC go to the seller.
//   SECRET=<64 hex> SELLER=0x… AMOUNT=<USDC units> \
//     npx hardhat run scripts/pay-local.js --network localhost
const fs = require("fs");
const { ethers } = require("hardhat");

async function main() {
  const { usdc: usdcAddr, vault: vaultAddr } = JSON.parse(fs.readFileSync(__dirname + "/../local.json"));
  const [buyer] = await ethers.getSigners();
  const usdc = await ethers.getContractAt("TestUSDC", usdcAddr);
  const vault = await ethers.getContractAt("ERC20SwapTimestamp", vaultAddr);
  const secret = "0x" + process.env.SECRET;
  const amount = BigInt(process.env.AMOUNT);
  const seller = process.env.SELLER;
  const hash = ethers.sha256(secret);
  const deadline = (await ethers.provider.getBlock("latest")).timestamp + 3600;
  await (await usdc.mint(buyer.address, amount)).wait();
  await (await usdc.approve(vaultAddr, amount)).wait();
  await (await vault["lock(bytes32,uint256,address,address,uint256)"](hash, amount, usdcAddr, seller, deadline)).wait();
  const tx = await vault["claim(bytes32,uint256,address,address,address,uint256)"](secret, amount, usdcAddr, seller, buyer.address, deadline);
  await tx.wait();
  console.log(`paid ${amount} to ${seller}: ${tx.hash}`);
  console.log(`seller USDC: ${await usdc.balanceOf(seller)}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
