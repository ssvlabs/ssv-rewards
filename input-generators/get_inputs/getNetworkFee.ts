import * as dotenv from 'dotenv';
import { ethers } from 'ethers';

dotenv.config();

const CURRENT_MONTH = process.env.CURRENT_MONTH;
const NETWORK_CONTRACT = process.env.NETWORK_CONTRACT;
const VIEWS_CONTRACT = process.env.VIEWS_CONTRACT;
const RPC_URL = process.env.RPC_URL || 'https://ethereum.publicnode.com';

interface NetworkFeeUpdate {
  blockNumber: number;
  timestamp: Date;
  transactionHash: string;
  previousFee: string;
  newFee: string;
}

interface ScanSummary {
  startDate: string;
  endDate: string;
  startBlock: number;
  endBlock: number;
  totalBlocks: number;
  updateCount: number;
  networkFeeInput: string;
}

// ABI for the updateNetworkFee event
const CONTRACT_ABI = [
  "event updateNetworkFee(uint256 previousFee, uint256 newFee)",
  "function updateNetworkFee(uint256 fee) external"
];

// ABI for the views contract
const VIEWS_ABI = [
  "function getNetworkFee() external view returns (uint256)"
];

function getMonthDateRange(yearMonth: string): { startDate: Date; endDate: Date } {
  const [year, month] = yearMonth.split('-').map(Number);
  const startDate = new Date(Date.UTC(year, month - 1, 1, 0, 0, 0));
  const endDate = new Date(Date.UTC(year, month, 0, 23, 59, 59, 999));
  
  return { startDate, endDate };
}

async function getBlockWithRetry(provider: ethers.Provider, blockNumber: number | string, maxRetries: number = 3): Promise<ethers.Block | null> {
  for (let attempt = 0; attempt < maxRetries; attempt++) {
    try {
      const block = await provider.getBlock(blockNumber);
      return block;
    } catch (error) {
      console.warn(`⚠️  Retry ${attempt + 1}/${maxRetries} for block ${blockNumber}: ${error}`);
      if (attempt === maxRetries - 1) throw error;
      
      const delay = Math.pow(2, attempt) * 1000;
      await new Promise(resolve => setTimeout(resolve, delay));
    }
  }
  return null;
}

async function getBlockByTimestamp(provider: ethers.Provider, targetTimestamp: number, isStart: boolean): Promise<number> {
  console.log(`🔍 Finding ${isStart ? 'start' : 'end'} block for timestamp ${new Date(targetTimestamp * 1000).toISOString()}`);
  
  const latestBlock = await getBlockWithRetry(provider, 'latest');
  if (!latestBlock) throw new Error('Could not get latest block');
  
  // Ethereum average block time is ~12 seconds
  const AVERAGE_BLOCK_TIME = 12;
  const timeDiff = latestBlock.timestamp - targetTimestamp;
  const estimatedBlocksBack = Math.floor(timeDiff / AVERAGE_BLOCK_TIME);
  let estimatedBlockNumber = latestBlock.number - estimatedBlocksBack;
  
  estimatedBlockNumber = Math.max(1, estimatedBlockNumber);
  
  // Binary search for the exact block
  let low = Math.max(1, estimatedBlockNumber - 5000);
  let high = Math.min(latestBlock.number, estimatedBlockNumber + 5000);
  let result = isStart ? high : low;
  let iterations = 0;
  
  while (low <= high && iterations < 20) {
    iterations++;
    const mid = Math.floor((low + high) / 2);
    
    const block = await getBlockWithRetry(provider, mid);
    if (!block) {
      console.warn(`   Could not fetch block ${mid}, skipping`);
      break;
    }
    
    const blockTimestamp = block.timestamp;
    
    if (isStart) {
      if (blockTimestamp >= targetTimestamp) {
        result = mid;
        high = mid - 1;
      } else {
        low = mid + 1;
      }
    } else {
      if (blockTimestamp <= targetTimestamp) {
        result = mid;
        low = mid + 1;
      } else {
        high = mid - 1;
      }
    }
    
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  
  const finalBlock = await getBlockWithRetry(provider, result);
  if (finalBlock) {
    console.log(`✅ Found ${isStart ? 'start' : 'end'} block: ${result}`);
  }
  
  return result;
}

function formatDate(date: Date): string {
  return date.toISOString().replace('T', ' ').replace(/\\.\\d{3}Z$/, ' UTC');
}

function formatDateShort(date: Date): string {
  return date.toISOString().split('T')[0];
}

async function getCurrentNetworkFee(provider: ethers.Provider): Promise<string> {
  console.log('📞 Calling views contract to get current network fee');
  const viewsContract = new ethers.Contract(VIEWS_CONTRACT!, VIEWS_ABI, provider);
  const networkFee = await viewsContract.getNetworkFee();
  console.log(`✅ Current network fee from views contract: ${networkFee.toString()}`);
  return networkFee.toString();
}

async function findNetworkFeeFromFuture(provider: ethers.Provider, startBlock: number): Promise<string | null> {
  console.log('🔍 Scanning from end of month to now for network fee changes');
  
  const latestBlock = await getBlockWithRetry(provider, 'latest');
  if (!latestBlock) return null;
  
  const CHUNK_SIZE = 10000;
  
  for (let fromBlock = startBlock; fromBlock <= latestBlock.number; fromBlock += CHUNK_SIZE) {
    const toBlock = Math.min(fromBlock + CHUNK_SIZE - 1, latestBlock.number);
    
    try {
      // Look for updateNetworkFee events using both approaches
      const contract = new ethers.Contract(NETWORK_CONTRACT!, CONTRACT_ABI, provider);
      const filter = contract.filters.updateNetworkFee();
      const standardEvents = await contract.queryFilter(filter, fromBlock, toBlock);
      
      const allLogs = await provider.getLogs({
        address: NETWORK_CONTRACT,
        fromBlock,
        toBlock
      });
      
      const multisigEvents = [];
      for (const log of allLogs) {
        try {
          if (log.data && log.data !== '0x' && log.data.length === 130) {
            const decoded = ethers.AbiCoder.defaultAbiCoder().decode(['uint256', 'uint256'], log.data);
            if (decoded.length === 2) {
              const previousFee = decoded[0].toString();
              const newFee = decoded[1].toString();
              
              const prevFeeNum = Number(previousFee);
              const newFeeNum = Number(newFee);
              
              if (prevFeeNum > 1e10 && newFeeNum > 1e10 && 
                  prevFeeNum < 1e15 && newFeeNum < 1e15 &&
                  Math.abs(Math.log10(newFeeNum) - Math.log10(prevFeeNum)) < 3) {
                multisigEvents.push({
                  blockNumber: log.blockNumber,
                  transactionHash: log.transactionHash,
                  args: decoded
                });
              }
            }
          }
        } catch (error) {
          // Ignore decode errors
        }
      }
      
      const combinedEvents = [...standardEvents, ...multisigEvents];
      if (combinedEvents.length > 0) {
        // Found a network fee change, return the old fee from the first event
        const event = combinedEvents[0];
        if ('args' in event && event.args) {
          const oldFee = event.args[0].toString();
          console.log(`✅ Found network fee change at block ${event.blockNumber}, old fee: ${oldFee}`);
          return oldFee;
        }
      }
      
      await new Promise(resolve => setTimeout(resolve, 200));
    } catch (error) {
      console.warn(`   ⚠️  Error scanning future blocks ${fromBlock}-${toBlock}: ${error}`);
    }
  }
  
  console.log('📋 No network fee changes found from end of month to now');
  return null;
}

function calculateNetworkFeeInput(startBlock: number, endBlock: number, updates: NetworkFeeUpdate[]): string {
  console.log('🧮 Calculating network fee input');
  
  if (updates.length === 0) {
    console.log('📊 No network fee changes in period - will need to find base fee');
    return '0'; // Placeholder - will be calculated later
  }
  
  // Sort updates by block number
  const sortedUpdates = [...updates].sort((a, b) => a.blockNumber - b.blockNumber);
  
  let totalFeeInput = BigInt(0);
  let currentBlock = startBlock;
  
  for (let i = 0; i < sortedUpdates.length; i++) {
    const update = sortedUpdates[i];
    const changeBlock = update.blockNumber;
    const previousFee = BigInt(update.previousFee);
    
    // Calculate fee input from currentBlock to changeBlock-1 using previous fee
    const blocksWithOldFee = BigInt(changeBlock - currentBlock);
    const feeInputSegment = blocksWithOldFee * previousFee;
    totalFeeInput += feeInputSegment;
    
    console.log(`📊 Blocks ${currentBlock} to ${changeBlock-1}: ${blocksWithOldFee.toString()} blocks * ${previousFee.toString()} wei = ${feeInputSegment.toString()} wei`);
    
    currentBlock = changeBlock;
  }
  
  // Handle remaining blocks from last change to end block
  if (currentBlock <= endBlock) {
    const lastUpdate = sortedUpdates[sortedUpdates.length - 1];
    const newFee = BigInt(lastUpdate.newFee);
    const remainingBlocks = BigInt(endBlock - currentBlock + 1);
    const finalSegment = remainingBlocks * newFee;
    totalFeeInput += finalSegment;
    
    console.log(`📊 Blocks ${currentBlock} to ${endBlock}: ${remainingBlocks.toString()} blocks * ${newFee.toString()} wei = ${finalSegment.toString()} wei`);
  }
  
  // Convert from wei to ETH (divide by 1e18)
  const totalFeeInputEth = Number(totalFeeInput) / 1e18;
  console.log(`📊 Total network fee input: ${totalFeeInput.toString()} wei = ${totalFeeInputEth.toFixed(18)}`);
  
  return totalFeeInputEth.toString();
}

function displaySummary(summary: ScanSummary): void {
  console.log('📊 Network Fee Scan Summary');
  console.log('============================');
  
  const table = [
    ['Item', 'Value'],
    ['─'.repeat(25), '─'.repeat(30)],
    ['Start Date', summary.startDate],
    ['End Date', summary.endDate],
    ['Start Block', summary.startBlock.toString()],
    ['End Block', summary.endBlock.toString()],
    ['Total Blocks to Scan', summary.totalBlocks.toLocaleString()],
    ['UpdateNetworkFee Updates', summary.updateCount.toString()],
    ['Network Fee Input', `${summary.networkFeeInput}`],
  ];
  
  const col1Width = Math.max(...table.map(row => row[0].length));
  const col2Width = Math.max(...table.map(row => row[1].length));
  
  table.forEach((row, index) => {
    if (index === 1) {
      console.log(`| ${row[0]} | ${row[1]} |`);
    } else {
      const col1 = row[0].padEnd(col1Width);
      const col2 = row[1].padEnd(col2Width);
      console.log(`| ${col1} | ${col2} |`);
    }
  });
}

function displayNetworkFeeUpdates(updates: NetworkFeeUpdate[]): void {  
  console.log('📋 Network Fee Updates');
  console.log('======================');
  
  const table = [
    ['Block', 'Date/Time', 'Previous Fee (wei)', 'New Fee (wei)', 'Tx Hash'],
    ['─'.repeat(10), '─'.repeat(20), '─'.repeat(18), '─'.repeat(15), '─'.repeat(66)],
  ];
  
  updates.forEach(update => {
    table.push([
      update.blockNumber.toString(),
      formatDate(update.timestamp),
      update.previousFee,
      update.newFee,
      update.transactionHash,
    ]);
  });
  
  const colWidths = [0, 1, 2, 3, 4].map(i => 
    Math.max(...table.map(row => row[i].length))
  );
  
  table.forEach((row, index) => {
    if (index === 1) {
      const separatorRow = colWidths.map((width) => '─'.repeat(width)).join(' | ');
      console.log(`| ${separatorRow} |`);
    } else {
      const formattedRow = row.map((cell, i) => cell.padEnd(colWidths[i])).join(' | ');
      console.log(`| ${formattedRow} |`);
    }
  });
}

async function analyzeSpecificTransaction(txHash: string): Promise<void> {
  console.log(`🔍 Analyzing transaction: ${txHash}`);
  
  const provider = new ethers.JsonRpcProvider(RPC_URL);
  
  try {
    const receipt = await provider.getTransactionReceipt(txHash);
    if (!receipt) {
      console.error('❌ Transaction not found');
      return;
    }
    
    console.log(`📋 Transaction found in block ${receipt.blockNumber}`);
    console.log(`🏷️  Total logs: ${receipt.logs.length}`);
    
    for (let i = 0; i < receipt.logs.length; i++) {
      const log = receipt.logs[i];
      console.log(`\\n📝 Log ${i + 1}:`);
      console.log(`   Address: ${log.address}`);
      console.log(`   Topics: ${log.topics}`);
      console.log(`   Data: ${log.data}`);
      
      if (log.address.toLowerCase() === NETWORK_CONTRACT?.toLowerCase()) {
        console.log(`   ✅ This log is from our target contract!`);
        
        // Check if this is an updateNetworkFee event by trying to decode it
        try {
          if (log.data && log.data !== '0x' && log.data.length > 2) {
            const decoded = ethers.AbiCoder.defaultAbiCoder().decode(['uint256', 'uint256'], log.data);
            if (decoded.length === 2) {
              console.log(`   🎯 Found updateNetworkFee event!`);
              console.log(`   📊 Decoded: ${decoded[0].toString()} -> ${decoded[1].toString()}`);
              
              if (decoded[1].toString() === '881260000000') {
                console.log(`   🎉 FOUND THE TARGET FEE!`);
              }
            }
          }
        } catch (error) {
          console.log(`   ⚠️  Could not decode as updateNetworkFee event: ${error}`);
        }
      }
    }
    
  } catch (error) {
    console.error('❌ Error analyzing transaction:', error);
  }
}

async function main(): Promise<void> {
  if (!CURRENT_MONTH) {
    console.error('❌ CURRENT_MONTH environment variable is not set');
    process.exit(1);
  }
  
  if (!NETWORK_CONTRACT) {
    console.error('❌ NETWORK_CONTRACT environment variable is not set');
    process.exit(1);
  }
  
  if (!VIEWS_CONTRACT) {
    console.error('❌ VIEWS_CONTRACT environment variable is not set');
    process.exit(1);
  }
  
  console.log(`🔍 Scanning network fee updates for ${CURRENT_MONTH}`);
  console.log(`📋 Network Contract: ${NETWORK_CONTRACT}`);
  console.log(`📋 Views Contract: ${VIEWS_CONTRACT}`);
  
  const { startDate, endDate } = getMonthDateRange(CURRENT_MONTH);
  const startTimestamp = Math.floor(startDate.getTime() / 1000);
  const endTimestamp = Math.floor(endDate.getTime() / 1000);
  
  console.log(`📅 Date range: ${formatDateShort(startDate)} to ${formatDateShort(endDate)}`);
  
  const provider = new ethers.JsonRpcProvider(RPC_URL);
  
  const startBlock = await getBlockByTimestamp(provider, startTimestamp, true);
  const endBlock = await getBlockByTimestamp(provider, endTimestamp, false);
  
  console.log(`📦 Scanning blocks ${startBlock} to ${endBlock} for network fee updates`);
  
  // Query for updateNetworkFee events in chunks
  const CHUNK_SIZE = 10000;
  const totalBlocks = endBlock - startBlock + 1;
  
  console.log(`📊 Total blocks to scan: ${totalBlocks.toLocaleString()}`);
  
  const allEvents = [];
  
  for (let fromBlock = startBlock; fromBlock <= endBlock; fromBlock += CHUNK_SIZE) {
    const toBlock = Math.min(fromBlock + CHUNK_SIZE - 1, endBlock);
    
    try {
      // Approach 1: Use standard ethers contract filtering for updateNetworkFee events
      const contract = new ethers.Contract(NETWORK_CONTRACT!, CONTRACT_ABI, provider);
      const filter = contract.filters.updateNetworkFee();
      const standardEvents = await contract.queryFilter(filter, fromBlock, toBlock);
      
      // Approach 2: Also search for logs with two uint256 parameters from our contract
      // This catches events from multisig transactions that may have different topic hashes
      const allLogs = await provider.getLogs({
        address: NETWORK_CONTRACT,
        fromBlock,
        toBlock
      });
      
      const multisigEvents = [];
      for (const log of allLogs) {
        try {
          // Try to decode any log with 2 uint256 parameters (potential updateNetworkFee)
          if (log.data && log.data !== '0x' && log.data.length === 130) { // 0x + 2 * 32 bytes * 2 hex chars
            const decoded = ethers.AbiCoder.defaultAbiCoder().decode(['uint256', 'uint256'], log.data);
            if (decoded.length === 2) {
              // Check if this looks like fee values (reasonable range)
              const previousFee = decoded[0].toString();
              const newFee = decoded[1].toString();
              
              // Basic sanity check: fees should be reasonable numbers 
              const prevFeeNum = Number(previousFee);
              const newFeeNum = Number(newFee);
              
              // Filter for reasonable fee ranges: both values should be > 1e10 (reasonable fee range)
              // and the difference between them shouldn't be too extreme (not block numbers)
              if (prevFeeNum > 1e10 && newFeeNum > 1e10 && 
                  prevFeeNum < 1e15 && newFeeNum < 1e15 &&
                  Math.abs(Math.log10(newFeeNum) - Math.log10(prevFeeNum)) < 3) { // Within 3 orders of magnitude
                
                multisigEvents.push({
                  blockNumber: log.blockNumber,
                  transactionHash: log.transactionHash,
                  args: decoded
                });
              }
            }
          }
        } catch (error) {
          // Ignore logs that can't be decoded as uint256,uint256
        }
      }
      
      // Combine standard events and potential multisig events
      const combinedEvents = [...standardEvents, ...multisigEvents];
      
      for (const event of combinedEvents) {
        if ('args' in event && event.args) {
          const previousFee = event.args[0].toString();
          const newFee = event.args[1].toString();
          
          console.log(`✅ Found updateNetworkFee: ${previousFee} -> ${newFee} (block ${event.blockNumber})`);
          
          allEvents.push(event);
        }
      }
      
      await new Promise(resolve => setTimeout(resolve, 200));
    } catch (error) {
      console.warn(`   ⚠️  Error in chunk ${fromBlock}-${toBlock}: ${error}`);
    }
  }
  
  const updates: NetworkFeeUpdate[] = [];
  
  for (const event of allEvents) {
    if ('args' in event && event.args) {
      const block = await getBlockWithRetry(provider, event.blockNumber);
      if (block) {
        updates.push({
          blockNumber: event.blockNumber,
          timestamp: new Date(block.timestamp * 1000),
          transactionHash: event.transactionHash,
          previousFee: (event.args as any)[0].toString(),
          newFee: (event.args as any)[1].toString(),
        });
      }
    }
  }
  
  // Calculate network fee input
  let networkFeeInput: string;
  
  if (updates.length === 0) {
    console.log('🔍 No network fee changes found in period');
    
    // Try to find network fee from future scanning
    const futureNetworkFee = await findNetworkFeeFromFuture(provider, endBlock + 1);
    let baseNetworkFee: string;
    
    if (futureNetworkFee) {
      baseNetworkFee = futureNetworkFee;
    } else {
      // No future changes found, get current fee from views contract
      baseNetworkFee = await getCurrentNetworkFee(provider);
    }
    
    // Calculate: networkFee * totalBlocks, then convert wei to ETH
    const totalFeeInput = BigInt(baseNetworkFee) * BigInt(totalBlocks);
    const totalFeeInputEth = Number(totalFeeInput) / 1e18;
    networkFeeInput = totalFeeInputEth.toString();
    
    console.log(`📊 Network fee input: ${baseNetworkFee} wei/block * ${totalBlocks} blocks = ${totalFeeInput.toString()} wei = ${totalFeeInputEth.toFixed(18)}`);
  } else {
    console.log('🔍 Network fee changes found in period');
    networkFeeInput = calculateNetworkFeeInput(startBlock, endBlock, updates);
  }
  
  const summary: ScanSummary = {
    startDate: formatDateShort(startDate),
    endDate: formatDateShort(endDate),
    startBlock,
    endBlock,
    totalBlocks,
    updateCount: updates.length,
    networkFeeInput
  };
  
  console.log('✅ Network fee scan complete!');
  console.log('')
  displaySummary(summary);
  console.log('')
  displayNetworkFeeUpdates(updates);
  
}

if (require.main === module) {
  // Check if we have a transaction hash as argument
  const txHash = process.argv[2];
  if (txHash && txHash.startsWith('0x')) {
    console.log('🎯 Analyzing specific transaction');
    analyzeSpecificTransaction(txHash).catch(error => {
      console.error('❌ Error:', error);
      process.exit(1);
    });
  } else {
    main().catch(error => {
      console.error('❌ Error:', error);
      process.exit(1);
    });
  }
}