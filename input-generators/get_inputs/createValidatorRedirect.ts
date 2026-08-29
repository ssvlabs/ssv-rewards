import * as dotenv from 'dotenv';
import { DuneClient, QueryParameter } from "@duneanalytics/client-sdk";
import { writeFileSync } from 'fs';

dotenv.config();

const CURRENT_MONTH = process.env.CURRENT_MONTH;
const DUNE_API_KEY = process.env.DUNE_API_KEY;

if (!CURRENT_MONTH) {
    console.error('❌ CURRENT_MONTH environment variable is not set');
    process.exit(1);
}

if (!DUNE_API_KEY) {
    console.error('❌ DUNE_API_KEY environment variable is not set');
    console.error('   Please add your Dune Analytics API key to the .env file');
    process.exit(1);
}

interface ValidatorResult {
    pubkey: string;
    module_name: string;
    activation_epoch?: number;
    activation_timestamp?: string;
}

function getMonthDateRange(yearMonth: string): { startDate: Date; endDate: Date } {
    const [year, month] = yearMonth.split('-').map(Number);
    const startDate = new Date(year, month - 1, 1); // First day of month
    const endDate = new Date(year, month, 0); // Last day of month
    
    return { startDate, endDate };
}

function formatDateForDune(date: Date): string {
    // Format as YYYY-MM-DD for Dune SQL queries
    return date.toISOString().split('T')[0];
}

function getNextMonth(yearMonth: string): string {
    const [year, month] = yearMonth.split('-').map(Number);
    
    // Calculate next month with year rollover
    let nextMonth = month + 1;
    let nextYear = year;
    
    if (nextMonth > 12) {
        nextMonth = 1;
        nextYear = year + 1;
    }
    
    // Format as YYYY-MM with leading zero for month
    return `${nextYear}-${nextMonth.toString().padStart(2, '0')}`;
}

function isValidatorAddedInMonth(validator: ValidatorResult, targetMonth: string): boolean {
    if (!validator.activation_timestamp) {
        console.log(`⚠️  Validator ${validator.pubkey.substring(0, 12)}... has no activation timestamp, including it`);
        return false; // If no timestamp, assume it was added before
    }
    
    const activationDate = new Date(validator.activation_timestamp);
    const { startDate, endDate } = getMonthDateRange(targetMonth);
    
    return activationDate >= startDate && activationDate <= endDate;
}

async function fetchValidatorsFromDune(): Promise<ValidatorResult[]> {
    console.log('🔍 Fetching validator data from Dune Analytics...');
    
    const dune = new DuneClient(DUNE_API_KEY!);
    
    // Calculate runMonth as current month + 1 with year rollover
    const runMonth = getNextMonth(CURRENT_MONTH!);
    console.log(`🔄 Using run_month: ${runMonth} (${CURRENT_MONTH} + 1 month)`);
    
    try {
        // Execute the query with parameters
        const query_result = await dune.runQuery({
            queryId: 6022703,
            query_parameters: [
                QueryParameter.text('run_month', runMonth)
            ]
        });
        
        console.log('📊 Query result status:', query_result.state);
        
        if (!query_result.result || !query_result.result.rows) {
            console.error('❌ No data returned from Dune query');
            return [];
        }
        
        const validators: ValidatorResult[] = query_result.result.rows.map((row: any) => ({
            pubkey: row.pubkey,
            module_name: row.module_name || 'Unknown',
            activation_epoch: row.activation_epoch,
            activation_timestamp: row.activation_timestamp
        }));
        
        console.log(`✅ Successfully fetched ${validators.length} validators from Dune`);
        return validators;
        
    } catch (error: any) {
        // Check if it's a rate limit error or similar
        if (error.message?.includes('10000') || error.message?.includes('limit')) {
            console.log('⚠️  API limit detected, this might require pagination or smaller queries');
            console.log('   Current implementation fetches the latest cached result');
        }
        
        console.error('❌ Error fetching data from Dune:', error.message || error);
        throw error;
    }
}

function filterValidatorsByDate(validators: ValidatorResult[], currentMonth: string): {
    validValidators: ValidatorResult[];
    addedInMonth: ValidatorResult[];
} {
    console.log(`🗓️  Filtering validators for month: ${currentMonth}`);
    
    const { startDate } = getMonthDateRange(currentMonth);
    const monthStart = startDate.getTime();
    
    // We want validators that were activated BEFORE the END of the current month
    // If running on August 5th with CURRENT_MONTH=2025-07, we want:
    // - All validators added before July 1st, 2025
    // - All validators added during July 2025
    // - None of the validators added in August 2025
    
    const { endDate } = getMonthDateRange(currentMonth);
    const monthEnd = new Date(endDate);
    monthEnd.setHours(23, 59, 59, 999); // End of the last day of the month
    
    const validValidators: ValidatorResult[] = [];
    const addedInMonth: ValidatorResult[] = [];
    
    for (const validator of validators) {
        if (!validator.activation_timestamp) {
            // If no activation timestamp, assume it was added before our target month
            validValidators.push(validator);
            continue;
        }
        
        const activationDate = new Date(validator.activation_timestamp);
        
        // Include if activated before or during the target month
        if (activationDate <= monthEnd) {
            validValidators.push(validator);
            
            // Check if it was added specifically during the target month
            if (activationDate >= startDate) {
                addedInMonth.push(validator);
            }
        }
    }
    
    return { validValidators, addedInMonth };
}

function createRedirectCSV(validators: ValidatorResult[], currentMonth: string): string {
    const fileName = `validator-redirects-${currentMonth}.csv`;
    const filePath = `data/current/${fileName}`;
    
    console.log(`📝 Creating CSV file: ${filePath}`);
    
    // Create CSV content
    const csvHeader = 'from,to\n';
    const redirectAddress = '0x13006e447608bB62383D1d59bb11a93e957Be7cF';
    
    const csvContent = csvHeader + validators
        .map(validator => `${validator.pubkey},${redirectAddress}`)
        .join('\n');
    
    // Write to file
    writeFileSync(filePath, csvContent, 'utf8');
    
    console.log(`✅ CSV file created successfully: ${fileName}`);
    console.log(`📊 Total validators in redirect file: ${validators.length}`);
    
    return filePath;
}

async function main(): Promise<void> {
    if (!CURRENT_MONTH) {
        console.error('❌ CURRENT_MONTH environment variable is not set');
        process.exit(1);
    }
    
    console.log('🚀 Starting Validator Redirect Creation');
    console.log('====================================');
    console.log(`📅 Current Month: ${CURRENT_MONTH}`);
    
    try {
        // Fetch validators from Dune
        const allValidators = await fetchValidatorsFromDune();
        
        if (allValidators.length === 0) {
            console.error('❌ No validators returned from Dune query');
            process.exit(1);
        }
        
        // Filter validators by date criteria
        const { validValidators, addedInMonth } = filterValidatorsByDate(allValidators, CURRENT_MONTH!);
        
        console.log('\n📊 FILTERING RESULTS:');
        console.log(`📋 Total validators from Dune: ${allValidators.length}`);
        console.log(`✅ Validators valid for ${CURRENT_MONTH}: ${validValidators.length}`);
        console.log(`🆕 Validators added in ${CURRENT_MONTH}: ${addedInMonth.length}`);
        
        // Create CSV file
        const csvPath = createRedirectCSV(validValidators, CURRENT_MONTH!);
        
        console.log('\n🎯 SUMMARY:');
        console.log(`📄 Created: ${csvPath}`);
        console.log(`📊 Total redirects: ${validValidators.length}`);
        console.log(`🆕 New validators in ${CURRENT_MONTH}: ${addedInMonth.length}`);
        console.log(`🔄 All validators redirect to: 0x13006e447608bB62383D1d59bb11a93e957Be7cF`);
        
        // Show sample of added validators if any
        if (addedInMonth.length > 0) {
            console.log(`\n📋 Sample validators added in ${CURRENT_MONTH}:`);
            addedInMonth.slice(0, 3).forEach((validator, index) => {
                console.log(`   ${index + 1}. ${validator.pubkey.substring(0, 16)}... (${validator.module_name})`);
            });
            if (addedInMonth.length > 3) {
                console.log(`   ... and ${addedInMonth.length - 3} more`);
            }
        }
        
        console.log('\n✅ Validator redirect creation completed successfully!');
        
    } catch (error: any) {
        console.error('❌ Error:', error.message || error);
        process.exit(1);
    }
}

if (require.main === module) {
    main().catch(error => {
        console.error('❌ Unexpected error:', error);
        process.exit(1);
    });
}