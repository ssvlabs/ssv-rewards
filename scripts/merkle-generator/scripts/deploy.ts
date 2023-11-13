import { ethers } from "hardhat";

async function main() {
  const tokenAddress='0x6471F70b932390f527c6403773D082A0Db8e8A9F';

  const Drop = await ethers.getContractFactory("CumulativeMerkleDrop");
  const drop = await Drop.deploy(tokenAddress);

  await drop.deployed();

  console.log(`Deployed to ${drop.address}`);
}

// We recommend this pattern to be able to use async/await everywhere
// and properly handle errors.
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
