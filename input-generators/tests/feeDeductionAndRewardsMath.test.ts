const fs2 = require('fs')
const path2 = require('path')
const Decimal2 = require('decimal.js')
require('dotenv').config();

// Configure Decimal.js for maximum precision
Decimal2.set({ precision: 100, rounding: Decimal2.ROUND_HALF_UP });

// Truncate to specific decimal places (no rounding)
function truncateToDecimalPlaces(value: any, decimals: number): any {
    // Use Decimal's built-in precision control for more consistent results
    const multiplier = new Decimal2(10).pow(decimals);
    return value.mul(multiplier).trunc().div(multiplier);
}

interface ValidatorData {
    RecipientAddress: string;
    OwnerAddress: string;
    PublicKey: string;
    ActiveDays: number;
    RegisteredDays: number;
    TotalActiveEffectiveBalance: number;
    TotalRegisteredEffectiveBalance: number;
    FeeDeduction: string;
    Reward: string;
}

interface AprBoostTier {
    max_effective_balance: number;
    apr_boost: number;
}

function parseCSV(csvData: string): ValidatorData[] {
    const lines = csvData.trim().split('\n');
    
    return lines.slice(1).map(line => {
        const values = line.split('\t');
        return {
            RecipientAddress: values[0],
            OwnerAddress: values[1], 
            PublicKey: values[2],
            ActiveDays: parseInt(values[3]),
            RegisteredDays: parseInt(values[4]),
            TotalActiveEffectiveBalance: parseInt(values[5]),
            TotalRegisteredEffectiveBalance: parseInt(values[6]),
            FeeDeduction: values[7],
            Reward: values[8]
        };
    });
}

function getDaysInMonth(yearMonth: string): number {
    const [year, month] = yearMonth.split('-').map(Number);
    return new Date(year, month, 0).getDate();
}

function calculateTotalEffectiveBalancePerDayAverage(validators: ValidatorData[]): any {
    let totalEffectiveBalance = new Decimal2(0);
    let totalDays = 0;
    let validatorCount = 0;
    
    for (const validator of validators) {
        if (validator.ActiveDays > 0) {
            totalEffectiveBalance = totalEffectiveBalance.add(validator.TotalActiveEffectiveBalance);
            totalDays += validator.ActiveDays;
            validatorCount++;
        }
    }
    
    if (totalDays === 0) {
        return new Decimal2(0);
    }
    
    // Calculate as: Total Balance / (Total Days / Validator Count)
    // This gives us the effective balance per average validator day
    const averageDaysPerValidator = new Decimal2(totalDays).div(validatorCount);
    const result = totalEffectiveBalance.div(averageDaysPerValidator);
    
    console.log(`Total Effective Balance: ${totalEffectiveBalance.toString()}`);
    console.log(`Total Days: ${totalDays}`);
    console.log(`Number of Validators: ${validatorCount}`);
    console.log(`Average Days per Validator: ${averageDaysPerValidator.toString()}`);
    console.log(`Result (Total Balance / Avg Days per Validator): ${result.toString()}`);
    
    return result;
}

function determineAprBoost(averageRatio: any, aprBoostTiers: AprBoostTier[]): any {
    // Sort tiers by max_effective_balance ascending
    const sortedTiers = aprBoostTiers.sort((a, b) => a.max_effective_balance - b.max_effective_balance);
    
    for (const tier of sortedTiers) {
        if (averageRatio.lte(tier.max_effective_balance)) {
            return new Decimal2(tier.apr_boost);
        }
    }
    
    // If above all tiers, use the last (highest) tier
    return new Decimal2(sortedTiers[sortedTiers.length - 1].apr_boost);
}

function calculateRewardTier(ethApr: any, ssvEth: any, aprBoost: any, days: number): any {
    // Formula: ((((32 * eth_apr) / ssv_eth) * apr_boost) / 365) * days
    const result = new Decimal2(32)
        .mul(ethApr)
        .div(ssvEth)
        .mul(aprBoost)
        .div(365)
        .mul(days);
    // Use truncateToDecimalPlaces for consistency instead of floor
    return truncateToDecimalPlaces(result, 18);
}

function calculateBaseReward(rewardTier: any, totalActiveEffectiveBalance: number, days: number): any {
    // Formula: (reward_tier * total_active_effective_balance) / (32 * days)
    const result = rewardTier
        .mul(totalActiveEffectiveBalance)
        .div(new Decimal2(32).mul(days));
    return truncateToDecimalPlaces(result, 18);
}

function calculateRawFee(
    networkFee: any, 
    totalRegisteredEffectiveBalance: number, 
    registeredDays: number, 
    days: number
): any {
    // Formula: max(((network_fee * total_registered_effective_balance) / (32 * days)) - ((network_fee * registered_days) / days), 0)
    const part1 = networkFee
        .mul(totalRegisteredEffectiveBalance)
        .div(new Decimal2(32).mul(days));
    
    const part2 = networkFee
        .mul(registeredDays)
        .div(days);
    
    const result = Decimal2.max(0, part1.sub(part2));
    return truncateToDecimalPlaces(result, 18);
}

function calculateFeeDeduction(baseReward: any, rawFee: any): any {
    // Formula: min(base_reward, raw_fee)
    const result = Decimal2.min(baseReward, rawFee);
    return truncateToDecimalPlaces(result, 18);
}

function calculateReward(baseReward: any, feeDeduction: any): any {
    // Formula: base_reward - fee_deduction
    const result = baseReward.sub(feeDeduction);
    return truncateToDecimalPlaces(result, 18);
}

function matchesAtDecimalPosition(val1: any, val2: any, decimalPosition: number): boolean {
    // Convert to string with full precision - no rounding
    const str1 = val1.toString();
    const str2 = val2.toString();
    
    // Find decimal points
    const dot1 = str1.indexOf('.');
    const dot2 = str2.indexOf('.');
    
    // Handle cases where there might be no decimal point
    const beforeDot1 = dot1 === -1 ? str1 : str1.substring(0, dot1);
    const beforeDot2 = dot2 === -1 ? str2 : str2.substring(0, dot2);
    
    if (beforeDot1 !== beforeDot2) return false;
    
    const afterDot1 = dot1 === -1 ? '' : str1.substring(dot1 + 1);
    const afterDot2 = dot2 === -1 ? '' : str2.substring(dot2 + 1);
    
    // Pad with zeros to ensure we can compare up to decimalPosition
    const padded1 = afterDot1.padEnd(decimalPosition, '0');
    const padded2 = afterDot2.padEnd(decimalPosition, '0');
    
    // Compare up to the specified decimal position
    return padded1.substring(0, decimalPosition) === padded2.substring(0, decimalPosition);
}

describe('Fee Deduction and Rewards Math Validation (Newest2)', () => {
    test('should match all validator calculations with MAX_SSV_REWARD applied first, then fee deductions (NEW APR BOOST LOGIC)', () => {
        process.stdout.write('\n🧮 Starting Fee Deduction and Rewards Math Validation (Newest2 - NEW APR BOOST LOGIC)...\n\n');
        
        // Load environment variables
        const ethApr = new Decimal2(process.env.CURRENT_ETH_APR!);
        const ssvEth = new Decimal2(process.env.CURRENT_SSV_ETH!);
        const networkFee = new Decimal2(process.env.CURRENT_NETWORK_FEE!); // Keep in ETH, not gwei
        const currentMonth = process.env.CURRENT_MONTH!;
        const aprBoostTiers: AprBoostTier[] = JSON.parse(process.env.CURRENT_APR_BOOST!);
        const maxSsvReward = new Decimal2(process.env.MAX_SSV_REWARD!);
        
        // Calculate days in current month
        const days = getDaysInMonth(currentMonth);
        
        // Load validator data
        const csvData = fs2.readFileSync(
            path2.join(__dirname, '../data/current/by-validator.csv'),
            'utf-8'
        );
        const validators = parseCSV(csvData);
        
        process.stdout.write(`📅 Current Month: ${currentMonth} (${days} days)\n`);
        process.stdout.write(`📊 ETH APR: ${ethApr.toString()}\n`);
        process.stdout.write(`📊 SSV/ETH: ${ssvEth.toString()}\n`);
        process.stdout.write(`📊 Network Fee: ${process.env.CURRENT_NETWORK_FEE}\n`);
        process.stdout.write(`📊 Max SSV Reward: ${maxSsvReward.toString()}\n`);
        process.stdout.write(`📋 Loaded ${validators.length} validators for validation\n\n`);
        
        // Calculate NEW APR boost logic: total effective balance / total days * validator count
        const averageRatio = calculateTotalEffectiveBalancePerDayAverage(validators);
        process.stdout.write(`📊 NEW LOGIC - Scaled Average Ratio: ${averageRatio.toFixed(2)}\n`);
        
        // Determine APR boost based on average ratio
        const aprBoost = determineAprBoost(averageRatio, aprBoostTiers);
        process.stdout.write(`📊 APR Boost: ${aprBoost.toString()} (${(aprBoost.toNumber() * 100).toFixed(1)}%)\n\n`);
        
        // Calculate reward tier (same for all validators)
        const rewardTier = calculateRewardTier(ethApr, ssvEth, aprBoost, days);
        process.stdout.write(`💰 Reward Tier: ${rewardTier.toFixed(18)}\n\n`);
        
        // First pass: Calculate all base rewards to check against MAX_SSV_REWARD
        process.stdout.write(`🔄 First pass: Calculating all base rewards to check against MAX_SSV_REWARD...\n`);
        let totalBaseRewards = new Decimal2(0);
        const validatorBaseRewards: Array<{ validator: ValidatorData, baseReward: any }> = [];
        
        for (const validator of validators) {
            const baseReward = calculateBaseReward(
                rewardTier, 
                validator.TotalActiveEffectiveBalance, 
                days
            );
            
            totalBaseRewards = totalBaseRewards.add(baseReward);
            validatorBaseRewards.push({
                validator,
                baseReward
            });
        }
        
        // Apply MAX_SSV_REWARD cap first
        let rewardRatio = new Decimal2(1);
        if (totalBaseRewards.gt(maxSsvReward)) {
            // Calculate ratio with truncation instead of pure division to match expected precision
            rewardRatio = truncateToDecimalPlaces(maxSsvReward.div(totalBaseRewards), 18);
            process.stdout.write(`🚨 Total base rewards (${totalBaseRewards.toFixed(18)}) exceed MAX_SSV_REWARD (${maxSsvReward.toString()})\n`);
            process.stdout.write(`📊 Applying ratio: ${rewardRatio.toFixed(18)} (${rewardRatio.mul(100).toFixed(4)}%)\n`);
        } else {
            process.stdout.write(`✅ Total base rewards (${totalBaseRewards.toFixed(18)}) within MAX_SSV_REWARD limit (${maxSsvReward.toString()})\n`);
            process.stdout.write(`📊 No ratio adjustment needed\n`);
        }
        
        // Calculate the capped total rewards (this should equal MAX_SSV_REWARD if cap was applied)
        const cappedTotalRewards = totalBaseRewards.mul(rewardRatio);
        process.stdout.write(`📊 Capped total rewards: ${cappedTotalRewards.toFixed(18)}\n\n`);
        
        // Now apply fee deductions to the capped rewards
        process.stdout.write(`🔄 Second pass: Applying fee deductions to capped rewards...\n`);
        let totalFinalRewards = new Decimal2(0);
        let totalNetworkFees = new Decimal2(0);
        const validatorResults: Array<{ 
            validator: ValidatorData, 
            cappedReward: any, 
            calculatedFeeDeduction: any, 
            finalReward: any 
        }> = [];
        
        for (const { validator, baseReward } of validatorBaseRewards) {
            // Apply the cap ratio to the base reward with higher precision, then truncate
            const cappedRewardHighPrecision = baseReward.mul(rewardRatio);
            const cappedReward = truncateToDecimalPlaces(cappedRewardHighPrecision, 18);
            
            // Calculate raw fee and fee deduction based on the ORIGINAL base reward (not capped)
            const rawFee = calculateRawFee(
                networkFee,
                validator.TotalRegisteredEffectiveBalance,
                validator.RegisteredDays,
                days
            );
            
            const calculatedFeeDeduction = calculateFeeDeduction(baseReward, rawFee);
            
            // Apply fee deduction to the capped reward with truncation at final step
            const finalRewardHighPrecision = Decimal2.max(0, cappedReward.sub(calculatedFeeDeduction));
            const finalReward = truncateToDecimalPlaces(finalRewardHighPrecision, 18);
            
            totalFinalRewards = totalFinalRewards.add(finalReward);
            totalNetworkFees = totalNetworkFees.add(calculatedFeeDeduction);
            
            validatorResults.push({
                validator,
                cappedReward,
                calculatedFeeDeduction,
                finalReward
            });
        }
        
        process.stdout.write(`📊 Total final rewards after fee deductions: ${totalFinalRewards.toFixed(18)}\n`);
        process.stdout.write(`📊 Total network fees deducted: ${totalNetworkFees.toFixed(18)}\n`);
        process.stdout.write(`📊 Sum (final rewards + fees): ${totalFinalRewards.add(totalNetworkFees).toFixed(18)}\n\n`);
        
        // Also calculate the total from the CSV for comparison
        let totalExpectedRewards = new Decimal2(0);
        for (const validator of validators) {
            totalExpectedRewards = totalExpectedRewards.add(new Decimal2(validator.Reward));
        }
        process.stdout.write(`📊 Total expected rewards from CSV: ${totalExpectedRewards.toFixed(18)}\n\n`);
        
        // Precision tracking counters for table
        const precisionCounters = {
            reward: { 18: 0, 17: 0, 16: 0, 15: 0, 14: 0, 13: 0, 12: 0 },
            fee: { 18: 0, 17: 0, 16: 0, 15: 0, 14: 0, 13: 0, 12: 0 }
        };
        
        let totalValidators = 0;
        let rewardMismatches = 0;
        let feeMismatches = 0;
        let totalRewardDifference = new Decimal2(0);
        let totalFeeDifference = new Decimal2(0);
        
        const fourteenthDecimalFailures: Array<{
            publicKey: string;
            type: 'reward' | 'feeDeduction';
            expected: string;
            calculated: string;
            difference: string;
        }> = [];
        
        // Validate each validator
        for (let i = 0; i < validatorResults.length; i++) {
            const { validator, finalReward, calculatedFeeDeduction } = validatorResults[i];
            totalValidators++;
            
            const expectedReward = new Decimal2(validator.Reward);
            const expectedFeeDeduction = new Decimal2(validator.FeeDeduction);
            
            
            // Track total differences
            const rewardDiff = finalReward.sub(expectedReward);
            const feeDiff = calculatedFeeDeduction.sub(expectedFeeDeduction);
            totalRewardDifference = totalRewardDifference.add(rewardDiff);
            totalFeeDifference = totalFeeDifference.add(feeDiff);
            
            // Check each decimal precision level and count matches/failures
            for (let decimals = 18; decimals >= 12; decimals--) {
                if (matchesAtDecimalPosition(finalReward, expectedReward, decimals)) {
                    // This validator matches at this precision level
                    precisionCounters.reward[decimals as keyof typeof precisionCounters.reward]++;
                }
            }
            
            // Only fail if doesn't match at 14th decimal
            if (!matchesAtDecimalPosition(finalReward, expectedReward, 14)) {
                rewardMismatches++;
                fourteenthDecimalFailures.push({
                    publicKey: validator.PublicKey,
                    type: 'reward',
                    expected: expectedReward.toFixed(18),
                    calculated: finalReward.toFixed(18),
                    difference: rewardDiff.toFixed(20)
                });
            }
            
            // Check each decimal precision level and count matches/failures
            for (let decimals = 18; decimals >= 12; decimals--) {
                if (matchesAtDecimalPosition(calculatedFeeDeduction, expectedFeeDeduction, decimals)) {
                    // This validator matches at this precision level
                    precisionCounters.fee[decimals as keyof typeof precisionCounters.fee]++;
                }
            }
            
            // Only fail if doesn't match at 14th decimal
            if (!matchesAtDecimalPosition(calculatedFeeDeduction, expectedFeeDeduction, 14)) {
                feeMismatches++;
                fourteenthDecimalFailures.push({
                    publicKey: validator.PublicKey,
                    type: 'feeDeduction',
                    expected: expectedFeeDeduction.toFixed(18),
                    calculated: calculatedFeeDeduction.toFixed(18),
                    difference: feeDiff.toFixed(20)
                });
            }
        }
        
        // Report results
        process.stdout.write(`📊 VALIDATION RESULTS (NEW APR BOOST LOGIC):\n`);
        process.stdout.write(`🎯 Reward mismatches: ${rewardMismatches} validators\n`);
        process.stdout.write(`💰 Fee deduction mismatches: ${feeMismatches} validators\n\n`);
        
        // Report total differences
        process.stdout.write(`📊 TOTAL DIFFERENCES:\n`);
        process.stdout.write(`🎯 TS math is ${totalRewardDifference.gte(0) ? 'higher' : 'lower'} than by-validator.csv by ${totalRewardDifference.abs().toFixed(18)} SSV (rewards)\n`);
        process.stdout.write(`💰 TS math is ${totalFeeDifference.gte(0) ? 'higher' : 'lower'} than by-validator.csv by ${totalFeeDifference.abs().toFixed(18)} SSV (fees)\n\n`);
        
        // Precision table showing mismatches with percentages
        process.stdout.write(`📊 PRECISION MISMATCH TABLE:\n`);
        process.stdout.write(`| Decimal | Reward Mismatches      | Fee Deduction Mismatches |\n`);
        process.stdout.write(`|---------|------------------------|---------------------------|\n`);
        for (let decimals = 18; decimals >= 12; decimals--) {
            const rewardMatches = precisionCounters.reward[decimals as keyof typeof precisionCounters.reward];
            const feeMatches = precisionCounters.fee[decimals as keyof typeof precisionCounters.fee];
            const rewardMismatches = totalValidators - rewardMatches;
            const feeMismatchesCount = totalValidators - feeMatches;
            const rewardMismatchPercent = ((rewardMismatches / totalValidators) * 100).toFixed(2);
            const feeMismatchPercent = ((feeMismatchesCount / totalValidators) * 100).toFixed(2);
            
            const rewardText = `${rewardMismatches.toLocaleString()} (${rewardMismatchPercent}%)`;
            const feeText = `${feeMismatchesCount.toLocaleString()} (${feeMismatchPercent}%)`;
            
            process.stdout.write(`| ${decimals}th     | ${rewardText.padStart(22)} | ${feeText.padStart(23)} |\n`);
        }
        process.stdout.write('\n');
        
        // Report first 10 14th decimal failures if any (before assertions to show details)  
        if (fourteenthDecimalFailures.length > 0) {
            process.stdout.write(`❌ VALIDATORS THAT FAILED AT 14th DECIMAL (${fourteenthDecimalFailures.length} total, showing first 10):\n\n`);
            fourteenthDecimalFailures.slice(0, 10).forEach((failure, index) => {
                process.stdout.write(`${index + 1}. Validator: ${failure.publicKey.substring(0, 16)}...\n`);
                process.stdout.write(`   Type: ${failure.type}\n`);
                process.stdout.write(`   Expected:   ${failure.expected}\n`);
                process.stdout.write(`   Calculated: ${failure.calculated}\n`);
                process.stdout.write(`   Difference: ${failure.difference}\n\n`);
            });
        }

        // Test assertion - fail only if any don't match at 14th decimal
        if (rewardMismatches > 0) {
            expect(rewardMismatches).toBe(0);
        }
        if (feeMismatches > 0) {
            expect(feeMismatches).toBe(0);
        }
        
        if (rewardMismatches === 0 && feeMismatches === 0) {
            process.stdout.write(`✅ ALL VALIDATIONS PASSED! 🎉\n`);
            process.stdout.write(`✅ All ${totalValidators} validators match to at least 14th decimal precision (NEW APR BOOST LOGIC)\n`);
        }
    });
});