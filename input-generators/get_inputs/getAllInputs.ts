import { exec } from 'child_process';
import { promisify } from 'util';
import dotenv from 'dotenv';

dotenv.config();

const execAsync = promisify(exec);

interface InputResult {
  name: string;
  value: string;
  success: boolean;
  error?: string;
}

async function runScript(scriptName: string, displayName: string): Promise<InputResult> {
  try {
    console.log(`Getting ${displayName}`);
    
    const { stdout, stderr } = await execAsync(`npx ts-node get_inputs/${scriptName}`, {
      cwd: process.cwd(),
      timeout: 300000 // 5 minute timeout
    });

    // Extract the input value from the output
    let inputValue = '';
    
    if (scriptName === 'getEthAPR.ts') {
      const match = stdout.match(/Eth APR Input\s+\|\s+([0-9.]+)/);
      inputValue = match ? match[1] : 'Not found';
    } else if (scriptName === 'getNetworkFee.ts') {
      const match = stdout.match(/Network Fee Input\s+\|\s+([0-9.]+)/);
      inputValue = match ? match[1] : 'Not found';
    } else if (scriptName === 'getSSVETH.ts') {
      const match = stdout.match(/SSV_ETH Input\s+\|\s+([0-9.]+)/);
      inputValue = match ? match[1] : 'Not found';
    }

    console.log(`${displayName} is ${inputValue}`);
    
    return {
      name: displayName,
      value: inputValue,
      success: true
    };
    
  } catch (error: any) {
    console.log(`❌ Error getting ${displayName}: ${error.message}`);
    return {
      name: displayName,
      value: 'Error',
      success: false,
      error: error.message
    };
  }
}

async function runCreateValidatorRedirect(): Promise<{ success: boolean; filePath?: string }> {
  try {
    console.log('Creating validator redirect file');
    
    const { stdout, stderr } = await execAsync('npx ts-node get_inputs/createValidatorRedirect.ts', {
      cwd: process.cwd(),
      timeout: 300000 // 5 minute timeout
    });

    // Extract the file path from the output
    const filePathMatch = stdout.match(/Created: (data\/current\/validator-redirects-[^.]+\.csv)/);
    const filePath = filePathMatch ? filePathMatch[1] : 'data/current/validator-redirects-*.csv';
    
    console.log(`Created validator redirect file successfully at ${filePath}`);
    return { success: true, filePath };
    
  } catch (error: any) {
    console.log(`❌ Error creating validator redirect file: ${error.message}`);
    return { success: false };
  }
}

async function main(): Promise<void> {
  const results: InputResult[] = [];

  // Run the three input collection scripts
  results.push(await runScript('getEthAPR.ts', 'ETH APR'));
  results.push(await runScript('getNetworkFee.ts', 'Network Fee'));  
  results.push(await runScript('getSSVETH.ts', 'SSV ETH'));

  // Run the validator redirect creation
  const redirectResult = await runCreateValidatorRedirect();

  // Display final results table
  console.log('\n📊 INPUT COLLECTION RESULTS');
  console.log('============================');

  const table = [
    ['Input Name', 'Input Value'],
    ['─'.repeat(15), '─'.repeat(20)]
  ];

  results.forEach(result => {
    if (result.success) {
      table.push([result.name, result.value]);
    } else {
      table.push([result.name, 'ERROR']);
    }
  });

  // Calculate column widths
  const col1Width = Math.max(...table.map(row => row[0].length));
  const col2Width = Math.max(...table.map(row => row[1].length));

  // Display table
  table.forEach((row, index) => {
    if (index === 1) {
      // Separator row
      console.log(`| ${row[0]} | ${row[1]} |`);
    } else {
      const col1 = row[0].padEnd(col1Width);
      const col2 = row[1].padEnd(col2Width);
      console.log(`| ${col1} | ${col2} |`);
    }
  });

  // Show any errors
  const errors = results.filter(r => !r.success);
  if (errors.length > 0) {
    console.log('\n❌ ERRORS:');
    errors.forEach(error => {
      console.log(`   ${error.name}: ${error.error}`);
    });
  }

  if (!redirectResult.success) {
    console.log('\n❌ Validator redirect file creation failed');
  }

  const successCount = results.filter(r => r.success).length;
  const totalInputs = results.length;
  
  console.log(`\n✅ Collection complete: ${successCount}/${totalInputs} inputs successful`);
  if (redirectResult.success && redirectResult.filePath) {
    console.log(`✅ Validator redirect file created successfully at ${redirectResult.filePath}`);
  }
}

if (require.main === module) {
  main().catch(error => {
    console.error('❌ Unexpected error:', error);
    process.exit(1);
  });
}