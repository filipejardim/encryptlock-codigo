require("@nomicfoundation/hardhat-toolbox");

// Deploying needs a funded key: AMOY_KEY (test POL only) for the Amoy test
// network; POLYGON_KEY for the real Polygon, a throwaway key made by
// scripts\cofre-polygon.ps1 that only pays the publishing (the vault has no
// owner: whoever publishes it has no power over it).
const networks = {};
if (process.env.AMOY_KEY) {
  networks.amoy = { url: "https://polygon-amoy-bor-rpc.publicnode.com", chainId: 80002, accounts: [process.env.AMOY_KEY] };
}
if (process.env.POLYGON_KEY) {
  networks.polygon = { url: "https://polygon-bor-rpc.publicnode.com", chainId: 137, accounts: [process.env.POLYGON_KEY] };
}

module.exports = {
  solidity: { version: "0.8.33", settings: { optimizer: { enabled: true, runs: 10000000 } } },
  networks,
};
