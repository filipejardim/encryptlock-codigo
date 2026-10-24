// Prints a new random Polygon key and its address as JSON (the throwaway key
// that pays for publishing the vault). Run with plain node.
const { Wallet } = require("ethers");
const w = Wallet.createRandom();
console.log(JSON.stringify({ key: w.privateKey, address: w.address }));
