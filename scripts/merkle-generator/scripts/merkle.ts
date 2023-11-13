const fs = require('fs')
const path = require('path')
import { MerkleTree } from 'merkletreejs'
import keccak256 from 'keccak256';
import web3 from 'web3';

// Read JSON file

// Parse JSON 

async function makeDrop() {
    // Read the JSON file
    const data = fs.readFileSync(path.join(__dirname, 'input_1.json'), 'utf-8');

    // Parse the JSON, converting large integers to BigInt
    const leavesJson = JSON.parse(data);

    // Convert the JSON object to an array of [key, value] pairs
    const leavesArray = Object.entries(leavesJson);

    // Convert the array to the desired format
    const elements = leavesArray.map((leaf: any) =>
        leaf[0] + web3.utils.padLeft(web3.utils.toHex(leaf[1]), 64).substring(2)
    );
    console.log("elements", elements);

    const hashedElements = elements.map(keccak256).map(x => MerkleTree.bufferToHex(x));
    console.log("hashedElements", hashedElements);

    const tree = new MerkleTree(elements, keccak256, { hashLeaves: true, sortPairs: true });

    const leaves = tree.getHexLeaves();
    console.log("tree leaves", leaves);


    const leavesWithProofs = elements.map((leaf, index) => {
        const proof = tree.getHexProof(keccak256(leaf));
        console.log("proof", proof);

        return {
            address: elements[index].slice(0, 42),
            amount: BigInt(`0x${elements[index].slice(42).toString()}`),
            proof
        }
    });

    const outputData = {
        root: tree.getHexRoot(),
        data: leavesWithProofs
    };
    // Output final JSON
    console.log(JSON.stringify(outputData, (key, value) =>
        typeof value === 'bigint' ? value.toString() : value
    ));

    fs.writeFileSync('output_1.json', JSON.stringify(outputData, (key, value) =>
        typeof value === 'bigint' ? value.toString() : value
    ));
}

async function main() {
    await makeDrop();

}

main().catch((error) => {
    console.error(error);
    process.exitCode = 1;
});