const fs = require('fs')
const path = require('path')
import { MerkleTree } from 'merkletreejs'
import keccak256 from 'keccak256';
import web3 from 'web3';
require('dotenv').config();

async function createMerkleRoot(dataPath: string): Promise<string> {
    // Read the cumulative.json file
    const data = fs.readFileSync(dataPath, 'utf-8');
    const leavesJson = JSON.parse(data);

    // Convert the JSON object to an array of [address, amount] pairs
    const leavesArray = Object.entries(leavesJson);

    // Convert to the format needed for merkle tree: address + padded hex amount
    const elements = leavesArray.map((leaf: any) =>
        leaf[0] + web3.utils.padLeft(web3.utils.toHex(BigInt(leaf[1])), 64).substring(2)
    );

    // Create merkle tree
    const tree = new MerkleTree(elements, keccak256, { hashLeaves: true, sortPairs: true });

    return tree.getHexRoot();
}

describe('Merkle Root Generation Match', () => {
    test('should generate correct merkle roots matching environment variables', async () => {
        const previousMerkleRoot = await createMerkleRoot(path.join(__dirname, '../data/previous/cumulative.json'));
        const currentMerkleRoot = await createMerkleRoot(path.join(__dirname, '../data/current/cumulative.json'));

        // Display results in a table
        process.stdout.write('📊 Merkle Root Comparison\n');
        process.stdout.write('=========================\n');
        
        const table = [
            ['Type', 'Merkle Root'],
            ['─'.repeat(10), '─'.repeat(66)],
            ['Previous', previousMerkleRoot],
            ['Current', currentMerkleRoot],
        ];
        
        const col1Width = Math.max(...table.map(row => row[0].length));
        const col2Width = Math.max(...table.map(row => row[1].length));
        
        table.forEach((row, index) => {
            if (index === 1) {
                process.stdout.write(`| ${row[0]} | ${row[1]} |\n`);
            } else {
                const col1 = row[0].padEnd(col1Width);
                const col2 = row[1].padEnd(col2Width);
                process.stdout.write(`| ${col1} | ${col2} |\n`);
            }
        });

        // Assertions (normalized to lowercase for case-insensitive comparison)
        expect(previousMerkleRoot.toLowerCase()).toBe(process.env.PREVIOUS_MERKLE?.toLowerCase());
        expect(currentMerkleRoot.toLowerCase()).toBe(process.env.CURRENT_MERKLE?.toLowerCase());
    });

    test('Previous + Current Month Cumulative Match', async () => {
        // Read previous cumulative
        const previousCumulativeData = fs.readFileSync(path.join(__dirname, '../data/previous/cumulative.json'), 'utf-8');
        const previousCumulative = JSON.parse(previousCumulativeData);

        // Read current month rewards from by-recipient.csv
        const byRecipientData = fs.readFileSync(path.join(__dirname, '../data/current/by-recipient.csv'), 'utf-8');
        const lines = byRecipientData.trim().split('\n');
        const headers = lines[0].split('\t');
        const addressIndex = headers.indexOf('RecipientAddress');
        const rewardIndex = headers.indexOf('Reward');

        if (addressIndex === -1 || rewardIndex === -1) {
            throw new Error('Required columns not found in by-recipient.csv');
        }

        // Build expected cumulative by adding previous + current rewards
        const expectedCumulative: { [key: string]: string } = { ...previousCumulative };

        for (let i = 1; i < lines.length; i++) {
            const columns = lines[i].split('\t');
            const address = `0x${columns[addressIndex]}`;
            const rewardInEth = columns[rewardIndex];
            
            // Convert reward from ETH to wei with proper precision
            // Split on decimal to handle precision correctly
            const parts = rewardInEth.split('.');
            const wholePart = parts[0] || '0';
            const decimalPart = (parts[1] || '0').padEnd(18, '0').substring(0, 18);
            const rewardInWei = BigInt(wholePart + decimalPart);

            if (expectedCumulative[address]) {
                // Add to existing amount
                const existingAmount = BigInt(expectedCumulative[address]);
                expectedCumulative[address] = (existingAmount + rewardInWei).toString();
            } else {
                // New address
                expectedCumulative[address] = rewardInWei.toString();
            }
        }

        // Read actual current cumulative
        const actualCumulativeData = fs.readFileSync(path.join(__dirname, '../data/current/cumulative.json'), 'utf-8');
        const actualCumulative = JSON.parse(actualCumulativeData);

        // Compare addresses
        const expectedAddresses = new Set(Object.keys(expectedCumulative));
        const actualAddresses = new Set(Object.keys(actualCumulative));
        
        expect(expectedAddresses.size).toBe(actualAddresses.size);
        expect([...expectedAddresses].sort()).toEqual([...actualAddresses].sort());

        // Compare amounts for each address
        for (const address of expectedAddresses) {
            expect(actualCumulative[address]).toBe(expectedCumulative[address]);
        }
    });
});