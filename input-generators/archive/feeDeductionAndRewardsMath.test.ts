const fs = require('fs')
const path = require('path')
const Decimal = require('decimal.js')
require('dotenv').config();

// Configure Decimal.js for maximum precision
Decimal.set({ precision: 100, rounding: Decimal.ROUND_HALF_UP });

// Truncate to specific decimal places (no rounding)
function truncateToDecimalPlaces(value: any, decimals: number): any {
    const str = value.toString();
    const dotIndex = str.indexOf('.');
    
    if (dotIndex === -1) {
        // No decimal point, add zeros if needed
        return new Decimal(str + '.'.padEnd(decimals + 1, '0'));
    }
    
    const beforeDot = str.substring(0, dotIndex);
    const afterDot = str.substring(dotIndex + 1);
    
    // Truncate (don't round) to desired decimal places
    const truncatedAfterDot = afterDot.substring(0, decimals).padEnd(decimals, '0');
    
    return new Decimal(beforeDot + '.' + truncatedAfterDot);
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

function calculateSumOfEffectiveBalancePerDayRatios(validators: ValidatorData[]): any {
    let totalRatioSum = new Decimal(0);
    
    for (const validator of validators) {
        if (validator.ActiveDays > 0) {
            const ratio = new Decimal(validator.TotalActiveEffectiveBalance).div(validator.ActiveDays);
            totalRatioSum = totalRatioSum.add(ratio);
        }
    }
    
    return totalRatioSum;
}

function determineAprBoost(sumOfRatios: any, aprBoostTiers: AprBoostTier[]): any {
    // Sort tiers by max_effective_balance ascending
    const sortedTiers = aprBoostTiers.sort((a, b) => a.max_effective_balance - b.max_effective_balance);
    
    for (const tier of sortedTiers) {
        if (sumOfRatios.lte(tier.max_effective_balance)) {
            return new Decimal(tier.apr_boost);
        }
    }
    
    // If above all tiers, use the last (highest) tier
    return new Decimal(sortedTiers[sortedTiers.length - 1].apr_boost);
}

function calculateRewardTier(ethApr: any, ssvEth: any, aprBoost: any, days: number): any {
    // Formula: ((((32 * eth_apr) / ssv_eth) * apr_boost) / 365) * days
    const result = new Decimal(32)
        .mul(ethApr)
        .div(ssvEth)
        .mul(aprBoost)
        .div(365)
        .mul(days);
    // Try floor instead of truncate for reward tier
    const multiplier = new Decimal(10).pow(18);
    return result.mul(multiplier).floor().div(multiplier);
}

function calculateBaseReward(rewardTier: any, totalActiveEffectiveBalance: number, days: number): any {
    // Formula: (reward_tier * total_active_effective_balance) / (32 * days)
    const result = rewardTier
        .mul(totalActiveEffectiveBalance)
        .div(new Decimal(32).mul(days));
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
        .div(new Decimal(32).mul(days));
    
    const part2 = networkFee
        .mul(registeredDays)
        .div(days);
    
    const result = Decimal.max(0, part1.sub(part2));
    return truncateToDecimalPlaces(result, 18);
}

function calculateFeeDeduction(baseReward: any, rawFee: any): any {
    // Formula: min(base_reward, raw_fee)
    const result = Decimal.min(baseReward, rawFee);
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

describe('Fee Deduction and Rewards Math Validation', () => {
    test('should match all validator calculations with proper APR boost detection', () => {
        process.stdout.write('\n🧮 Starting Fee Deduction and Rewards Math Validation...\n\n');
        
        // Load environment variables
        const ethApr = new Decimal(process.env.CURRENT_ETH_APR!);
        const ssvEth = new Decimal(process.env.CURRENT_SSV_ETH!);
        const networkFee = new Decimal(process.env.CURRENT_NETWORK_FEE!); // Keep in ETH, not gwei
        const currentMonth = process.env.CURRENT_MONTH!;
        const aprBoostTiers: AprBoostTier[] = JSON.parse(process.env.CURRENT_APR_BOOST!);
        
        // Calculate days in current month
        const days = getDaysInMonth(currentMonth);
        
        // Load validator data
        const csvData = fs.readFileSync(
            path.join(__dirname, '../data/current/by-validator.csv'),
            'utf-8'
        );
        const validators = parseCSV(csvData);
        
        process.stdout.write(`📅 Current Month: ${currentMonth} (${days} days)\n`);
        process.stdout.write(`📊 ETH APR: ${ethApr.toString()}\n`);
        process.stdout.write(`📊 SSV/ETH: ${ssvEth.toString()}\n`);
        process.stdout.write(`📊 Network Fee: ${process.env.CURRENT_NETWORK_FEE}\n`);
        process.stdout.write(`📋 Loaded ${validators.length} validators for validation\n\n`);
        
        // Calculate sum of (effective balance / active days) for all validators
        const sumOfRatios = calculateSumOfEffectiveBalancePerDayRatios(validators);
        process.stdout.write(`📊 Sum of (Effective Balance / Active Days): ${sumOfRatios.toFixed(2)}\n`);
        
        // Determine APR boost based on sum of ratios
        const aprBoost = determineAprBoost(sumOfRatios, aprBoostTiers);
        process.stdout.write(`📊 APR Boost: ${aprBoost.toString()} (${(aprBoost.toNumber() * 100).toFixed(1)}%)\n\n`);
        
        // Calculate reward tier (same for all validators)
        const rewardTier = calculateRewardTier(ethApr, ssvEth, aprBoost, days);
        process.stdout.write(`💰 Reward Tier: ${rewardTier.toFixed(18)}\n\n`);
        
        // Precision tracking counters for table
        const precisionCounters = {
            reward: { 18: 0, 17: 0, 16: 0, 15: 0 },
            fee: { 18: 0, 17: 0, 16: 0, 15: 0 }
        };
        
        let totalValidators = 0;
        let rewardMismatches = 0;
        let feeMismatches = 0;
        let totalRewardDifference = new Decimal(0);
        let totalFeeDifference = new Decimal(0);
        
        const seventeenthDecimalFailures: Array<{
            publicKey: string;
            type: 'reward' | 'feeDeduction';
            expected: string;
            calculated: string;
            difference: string;
        }> = [];
        
        // Validate each validator
        for (const validator of validators) {
            totalValidators++;
            
            // Calculate values using the exact formulas
            const baseReward = calculateBaseReward(
                rewardTier, 
                validator.TotalActiveEffectiveBalance, 
                days
            );
            
            const rawFee = calculateRawFee(
                networkFee,
                validator.TotalRegisteredEffectiveBalance,
                validator.RegisteredDays,
                days
            );
            
            const calculatedFeeDeduction = calculateFeeDeduction(baseReward, rawFee);
            const calculatedReward = calculateReward(baseReward, calculatedFeeDeduction);
            
            const expectedReward = new Decimal(validator.Reward);
            const expectedFeeDeduction = new Decimal(validator.FeeDeduction);
            
            // Track total differences
            const rewardDiff = calculatedReward.sub(expectedReward);
            const feeDiff = calculatedFeeDeduction.sub(expectedFeeDeduction);
            totalRewardDifference = totalRewardDifference.add(rewardDiff);
            totalFeeDifference = totalFeeDifference.add(feeDiff);
            
            // Check each decimal precision level and count matches/failures
            for (let decimals = 18; decimals >= 15; decimals--) {
                if (matchesAtDecimalPosition(calculatedReward, expectedReward, decimals)) {
                    // This validator matches at this precision level
                    precisionCounters.reward[decimals as keyof typeof precisionCounters.reward]++;
                }
            }
            
            // Only fail if doesn't match at 15th decimal
            if (!matchesAtDecimalPosition(calculatedReward, expectedReward, 15)) {
                rewardMismatches++;
                seventeenthDecimalFailures.push({
                    publicKey: validator.PublicKey,
                    type: 'reward',
                    expected: expectedReward.toFixed(18),
                    calculated: calculatedReward.toFixed(18),
                    difference: rewardDiff.toFixed(20)
                });
            }
            
            // Check each decimal precision level and count matches/failures
            for (let decimals = 18; decimals >= 15; decimals--) {
                if (matchesAtDecimalPosition(calculatedFeeDeduction, expectedFeeDeduction, decimals)) {
                    // This validator matches at this precision level
                    precisionCounters.fee[decimals as keyof typeof precisionCounters.fee]++;
                }
            }
            
            // Only fail if doesn't match at 15th decimal
            if (!matchesAtDecimalPosition(calculatedFeeDeduction, expectedFeeDeduction, 15)) {
                feeMismatches++;
                seventeenthDecimalFailures.push({
                    publicKey: validator.PublicKey,
                    type: 'feeDeduction',
                    expected: expectedFeeDeduction.toFixed(18),
                    calculated: calculatedFeeDeduction.toFixed(18),
                    difference: feeDiff.toFixed(20)
                });
            }
        }
        
        // Report results
        process.stdout.write(`📊 VALIDATION RESULTS:\n`);
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
        for (let decimals = 18; decimals >= 15; decimals--) {
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
        
        // Report first 10 15th decimal failures if any (before assertions to show details)
        if (seventeenthDecimalFailures.length > 0) {
            process.stdout.write(`❌ VALIDATORS THAT FAILED AT 15th DECIMAL (${seventeenthDecimalFailures.length} total, showing first 10):\n\n`);
            seventeenthDecimalFailures.slice(0, 10).forEach((failure, index) => {
                process.stdout.write(`${index + 1}. Validator: ${failure.publicKey.substring(0, 16)}...\n`);
                process.stdout.write(`   Type: ${failure.type}\n`);
                process.stdout.write(`   Expected:   ${failure.expected}\n`);
                process.stdout.write(`   Calculated: ${failure.calculated}\n`);
                process.stdout.write(`   Difference: ${failure.difference}\n\n`);
            });
        }

        // Test assertion - fail only if any don't match at 15th decimal
        if (rewardMismatches > 0) {
            expect(rewardMismatches).toBe(0);
        }
        if (feeMismatches > 0) {
            expect(feeMismatches).toBe(0);
        }
        
        if (rewardMismatches === 0 && feeMismatches === 0) {
            process.stdout.write(`✅ ALL VALIDATIONS PASSED! 🎉\n`);
            process.stdout.write(`✅ All ${totalValidators} validators match to at least 15th decimal precision\n`);
        }
    });
});