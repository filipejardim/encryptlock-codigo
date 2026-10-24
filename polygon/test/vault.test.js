// The swap vault (Boltz ERC20SwapTimestamp) used the way EncryptLock uses it:
// the buyer locks USDC for the seller with the hash of a secret; whoever shows
// the secret sends the USDC to the seller (the seller can be offline); without
// the secret the buyer gets the USDC back after the deadline.
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { time } = require("@nomicfoundation/hardhat-network-helpers");

const USDC = (n) => ethers.parseUnits(String(n), 6);
const HOUR = 3600;

async function setup() {
  const [buyer, seller, anyone] = await ethers.getSigners();
  const usdc = await (await ethers.getContractFactory("TestUSDC")).deploy();
  const vault = await (await ethers.getContractFactory("ERC20SwapTimestamp")).deploy();
  await usdc.mint(buyer.address, USDC(1000));
  await usdc.connect(buyer).approve(await vault.getAddress(), USDC(1000));
  const secret = ethers.randomBytes(32);
  const hash = ethers.sha256(secret);
  const deadline = (await time.latest()) + HOUR;
  return { buyer, seller, anyone, usdc, vault, secret, hash, deadline };
}

async function lock(s, amount = USDC(1000)) {
  await s.vault
    .connect(s.buyer)
    ["lock(bytes32,uint256,address,address,uint256)"](s.hash, amount, await s.usdc.getAddress(), s.seller.address, s.deadline);
}

function claimBy(s, who, secret = s.secret, amount = USDC(1000)) {
  return s.vault
    .connect(who)
    ["claim(bytes32,uint256,address,address,address,uint256)"](
      secret, amount, s.usdc.target, s.seller.address, s.buyer.address, s.deadline,
    );
}

function refundBy(s, who) {
  return s.vault
    .connect(who)
    ["refund(bytes32,uint256,address,address,address,uint256)"](
      s.hash, USDC(1000), s.usdc.target, s.seller.address, s.buyer.address, s.deadline,
    );
}

describe("swap vault", () => {
  it("complete trade: anyone with the secret pays the seller, and the secret becomes public", async () => {
    const s = await setup();
    await lock(s);
    expect(await s.usdc.balanceOf(s.buyer.address)).to.equal(0n);
    // The seller is offline: someone else (the buyer's app) shows the secret.
    await expect(claimBy(s, s.anyone))
      .to.emit(s.vault, "Claim")
      .withArgs(s.hash, ethers.hexlify(s.secret));
    expect(await s.usdc.balanceOf(s.seller.address)).to.equal(USDC(1000));
    expect(await s.usdc.balanceOf(s.anyone.address)).to.equal(0n);
  });

  it("a wrong secret pays nobody", async () => {
    const s = await setup();
    await lock(s);
    await expect(claimBy(s, s.anyone, ethers.randomBytes(32))).to.be.reverted;
  });

  it("failed trade: no refund before the deadline, full refund after it", async () => {
    const s = await setup();
    await lock(s);
    await expect(refundBy(s, s.buyer)).to.be.reverted;
    await time.increaseTo(s.deadline);
    await refundBy(s, s.anyone); // anyone may trigger it; the USDC goes to the buyer
    expect(await s.usdc.balanceOf(s.buyer.address)).to.equal(USDC(1000));
    expect(await s.usdc.balanceOf(s.seller.address)).to.equal(0n);
  });

  it("never pays twice: no claim after a refund, no refund after a claim", async () => {
    const a = await setup();
    await lock(a);
    await claimBy(a, a.anyone);
    await time.increaseTo(a.deadline);
    await expect(refundBy(a, a.buyer)).to.be.reverted;
    await expect(claimBy(a, a.anyone)).to.be.reverted;

    const b = await setup();
    await lock(b);
    await time.increaseTo(b.deadline);
    await refundBy(b, b.buyer);
    await expect(claimBy(b, b.anyone)).to.be.reverted;
  });

  it("the secret only works for the agreed amount, seller and deadline", async () => {
    const s = await setup();
    await lock(s);
    await expect(claimBy(s, s.anyone, s.secret, USDC(999))).to.be.reverted;
  });

  it("the buyer can send the seller a little POL for gas in the same step", async () => {
    const s = await setup();
    const before = await ethers.provider.getBalance(s.seller.address);
    await s.vault
      .connect(s.buyer)
      .lockPrepayMinerfee(s.hash, USDC(1000), s.usdc.target, s.seller.address, s.deadline, {
        value: ethers.parseEther("0.02"),
      });
    expect((await ethers.provider.getBalance(s.seller.address)) - before).to.equal(ethers.parseEther("0.02"));
  });
});
