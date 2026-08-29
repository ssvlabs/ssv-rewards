import { readFileSync, writeFileSync, existsSync } from 'fs';
import { join } from 'path';
import * as dotenv from 'dotenv';

const Decimal = require('decimal.js');
Decimal.set({ precision: 100, rounding: Decimal.ROUND_HALF_UP });

dotenv.config();

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

interface OwnerRedirect {
    from: string;
    to: string;
}

interface ValidatorRedirect {
    from: string;
    to: string;
}

interface TableSection {
    title: string;
    headers: string[];
    rows: string[][];
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

function parseRedirectCSV<T>(filePath: string, parser: (values: string[]) => T): T[] {
    if (!existsSync(filePath)) {
        return [];
    }
    const content = readFileSync(filePath, 'utf-8');
    const lines = content.trim().split('\n');
    return lines.slice(1).map(line => {
        const values = line.split(/[,\t]/);
        return parser(values);
    });
}

function getDaysInMonth(yearMonth: string): number {
    const [year, month] = yearMonth.split('-').map(Number);
    return new Date(year, month, 0).getDate();
}

function loadValidatorData(period: 'current' | 'previous'): ValidatorData[] {
    const csvData = readFileSync(
        join(__dirname, `../data/${period}/by-validator.csv`),
        'utf-8'
    );
    return parseCSV(csvData);
}

function calculateTotalEffectiveBalancePerDayAverage(validators: ValidatorData[]): any {
    let totalEffectiveBalance = new Decimal(0);
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
        return new Decimal(0);
    }

    // Calculate as: Total Balance / (Total Days / Validator Count)
    // This gives us the effective balance per average validator day
    const averageDaysPerValidator = new Decimal(totalDays).div(validatorCount);
    const result = totalEffectiveBalance.div(averageDaysPerValidator);

    console.log(`Total Effective Balance: ${totalEffectiveBalance.toString()}`);
    console.log(`Total Days: ${totalDays}`);
    console.log(`Number of Validators: ${validatorCount}`);
    console.log(`Average Days per Validator: ${averageDaysPerValidator.toString()}`);
    console.log(`Result (Total Balance / Avg Days per Validator): ${result.toString()}`);

    return result;
}

function determineAprBoost(averageRatio: any, aprBoostTiers: AprBoostTier[]): any {
    const sortedTiers = aprBoostTiers.sort((a, b) => a.max_effective_balance - b.max_effective_balance);

    for (const tier of sortedTiers) {
        if (averageRatio.lte(tier.max_effective_balance)) {
            return new Decimal(tier.apr_boost);
        }
    }

    return new Decimal(sortedTiers[sortedTiers.length - 1].apr_boost);

}

function calculateRewardTier(ethApr: any, ssvEth: any, aprBoost: any, days: number): any {
    return new Decimal(32)
        .mul(ethApr)
        .div(ssvEth)
        .mul(aprBoost)
        .div(365)
        .mul(days);
}

function loadRedirectStats(period: 'current' | 'previous') {
    const monthVar = period === 'current' ? 'CURRENT_MONTH' : 'PREVIOUS_MONTH';
    const month = process.env[monthVar];

    const validatorData = loadValidatorData(period);

    const ownerRedirects = parseRedirectCSV(
        join(__dirname, `../data/${period}/owner-redirects.csv`),
        (values) => ({ from: values[0], to: values[1] })
    );

    const validatorRedirects = parseRedirectCSV(
        join(__dirname, `../data/${period}/validator-redirects-${month}.csv`),
        (values) => ({ from: values[0], to: values[1] })
    );

    const normalizeAddress = (addr: string): string => {
        return addr.toLowerCase().replace(/^0x/, '');
    };

    const ownerRedirectMap = new Map<string, string>();
    ownerRedirects.forEach(redirect => {
        ownerRedirectMap.set(normalizeAddress(redirect.from), normalizeAddress(redirect.to));
    });

    const validatorRedirectMap = new Map<string, string>();
    validatorRedirects.forEach(redirect => {
        validatorRedirectMap.set(normalizeAddress(redirect.from), normalizeAddress(redirect.to));
    });

    let validatorsWithValidatorRedirects = 0;
    let validatorsWithOwnerRedirects = 0;
    let validatorsWithNoRedirects = 0;

    const ownerRedirectsActuallyUsed = new Set<string>();
    const validatorRedirectsActuallyUsed = new Set<string>();

    for (const validator of validatorData) {
        const ownerAddress = normalizeAddress(validator.OwnerAddress);
        const publicKey = normalizeAddress(validator.PublicKey);

        if (validatorRedirectMap.has(publicKey)) {
            validatorRedirectsActuallyUsed.add(publicKey);
            validatorsWithValidatorRedirects++;
        } else if (ownerRedirectMap.has(ownerAddress)) {
            ownerRedirectsActuallyUsed.add(ownerAddress);
            validatorsWithOwnerRedirects++;
        } else {
            validatorsWithNoRedirects++;
        }
    }

    const usedOwners = new Set(validatorData.map(v => normalizeAddress(v.OwnerAddress)));
    const usedValidators = new Set(validatorData.map(v => normalizeAddress(v.PublicKey)));

    return {
        totalValidators: validatorData.length,
        validatorsWithValidatorRedirects,
        validatorsWithOwnerRedirects,
        validatorsWithNoRedirects,
        declaredValidatorRedirects: validatorRedirects.length,
        declaredOwnerRedirects: ownerRedirects.length,
        totalOwnerAddresses: usedOwners.size,
        totalRecipientAddresses: new Set(validatorData.map(v => normalizeAddress(v.RecipientAddress))).size,
        redirectedOwners: ownerRedirectsActuallyUsed.size,
        ownersWithNoRedirects: ownerRedirects.filter(redirect =>
            !usedOwners.has(normalizeAddress(redirect.from))
        ).length
    };
}

function formatNumber(num: number | string): string {
    if (typeof num === 'string') {
        const parsed = parseFloat(num);
        return parsed.toLocaleString();
    }
    return num.toLocaleString();
}

function formatPercentage(num: number, decimals: number = 2): string {
    return `${num.toFixed(decimals)}%`;
}

function calculateDifference(current: number | string, previous: number | string): string {
    const curr = typeof current === 'string' ? parseFloat(current) : current;
    const prev = typeof previous === 'string' ? parseFloat(previous) : previous;
    return (curr - prev).toFixed(6);
}

function generateOverallSection(): TableSection {
    console.log('Creating metrics...');

    const currentMonth = process.env.CURRENT_MONTH!;
    const previousMonth = process.env.PREVIOUS_MONTH!;

    const currentData = loadValidatorData('current');
    const previousData = loadValidatorData('previous');

    // Calculate sums for current
    const currentActiveBalance = currentData.reduce((sum, v) => sum + v.TotalActiveEffectiveBalance, 0);
    const currentRegisteredBalance = currentData.reduce((sum, v) => sum + v.TotalRegisteredEffectiveBalance, 0);
    const currentRewards = currentData.reduce((sum, v) => sum + parseFloat(v.Reward), 0);
    const currentFeeDeductions = currentData.reduce((sum, v) => sum + parseFloat(v.FeeDeduction), 0);
    const currentTotalRewardDistribution = currentFeeDeductions + currentRewards;
    const currentOwners = new Set(currentData.map(v => v.OwnerAddress)).size;
    const currentRecipients = new Set(currentData.map(v => v.RecipientAddress)).size;

    // Calculate sums for previous
    const previousActiveBalance = previousData.reduce((sum, v) => sum + v.TotalActiveEffectiveBalance, 0);
    const previousRegisteredBalance = previousData.reduce((sum, v) => sum + v.TotalRegisteredEffectiveBalance, 0);
    const previousRewards = previousData.reduce((sum, v) => sum + parseFloat(v.Reward), 0);
    const previousFeeDeductions = previousData.reduce((sum, v) => sum + parseFloat(v.FeeDeduction), 0);
    const previousTotalRewardDistribution = previousFeeDeductions + previousRewards;
    const previousOwners = new Set(previousData.map(v => v.OwnerAddress)).size;
    const previousRecipients = new Set(previousData.map(v => v.RecipientAddress)).size;

    return {
        title: 'Overall',
        headers: ['Item', 'Current Month', 'Previous Month', 'Difference (Current - Previous)'],
        rows: [
            ['Months', currentMonth, previousMonth, 'NA'],
            ['Validator count', formatNumber(currentData.length), formatNumber(previousData.length), calculateDifference(currentData.length, previousData.length)],
            ['Sum of active effective balance', formatNumber(currentActiveBalance), formatNumber(previousActiveBalance), calculateDifference(currentActiveBalance, previousActiveBalance)],
            ['Sum of registered effective balance', formatNumber(currentRegisteredBalance), formatNumber(previousRegisteredBalance), calculateDifference(currentRegisteredBalance, previousRegisteredBalance)],
            ['Sum of rewards', currentRewards.toFixed(6), previousRewards.toFixed(6), calculateDifference(currentRewards, previousRewards)],
            ['DAO reward', currentFeeDeductions.toFixed(6), previousFeeDeductions.toFixed(6), calculateDifference(currentFeeDeductions, previousFeeDeductions)],
            ['Total Reward Distribution', currentTotalRewardDistribution.toFixed(6), previousTotalRewardDistribution.toFixed(6), calculateDifference(currentTotalRewardDistribution, previousTotalRewardDistribution)],
            ['Inflation Cap', formatNumber(process.env.MAX_SSV_REWARD!), formatNumber(process.env.MAX_SSV_REWARD!), 'NA'],
            ['How many unique owner addresses', formatNumber(currentOwners), formatNumber(previousOwners), calculateDifference(currentOwners, previousOwners)],
            ['How many unique recipient addresses', formatNumber(currentRecipients), formatNumber(previousRecipients), calculateDifference(currentRecipients, previousRecipients)],
            ['Merkle', process.env.CURRENT_MERKLE!, process.env.PREVIOUS_MERKLE!, 'NA']
        ]
    };
}

function generateInputsSection(): TableSection {
    const currentMonth = process.env.CURRENT_MONTH!;
    const previousMonth = process.env.PREVIOUS_MONTH!;

    const currentDays = getDaysInMonth(currentMonth);
    const previousDays = getDaysInMonth(previousMonth);

    const currentEthApr = parseFloat(process.env.CURRENT_ETH_APR!);
    const previousEthApr = parseFloat(process.env.PREVIOUS_ETH_APR!);

    const currentSsvEth = parseFloat(process.env.CURRENT_SSV_ETH!);
    const previousSsvEth = parseFloat(process.env.PREVIOUS_SSV_ETH!);

    const currentNetworkFee = parseFloat(process.env.CURRENT_NETWORK_FEE!);
    const previousNetworkFee = parseFloat(process.env.PREVIOUS_NETWORK_FEE!);

    // Calculate APR boost for both months using new logic
    const currentValidators = loadValidatorData('current');
    const previousValidators = loadValidatorData('previous');

    const currentAprBoostTiers: AprBoostTier[] = JSON.parse(process.env.CURRENT_APR_BOOST!);
    const previousAprBoostTiers: AprBoostTier[] = JSON.parse(process.env.PREVIOUS_APR_BOOST!);

    const currentAverageRatio = calculateTotalEffectiveBalancePerDayAverage(currentValidators);
    const previousAverageRatio = calculateTotalEffectiveBalancePerDayAverage(previousValidators);

    const currentAprBoost = determineAprBoost(currentAverageRatio, currentAprBoostTiers).toNumber();
    const previousAprBoost = determineAprBoost(previousAverageRatio, previousAprBoostTiers).toNumber();

    // Calculate reward tier
    const currentRewardTier = calculateRewardTier(new Decimal(currentEthApr), new Decimal(currentSsvEth), new Decimal(currentAprBoost), currentDays).toNumber();
    const previousRewardTier = calculateRewardTier(new Decimal(previousEthApr), new Decimal(previousSsvEth), new Decimal(previousAprBoost), previousDays).toNumber();

    return {
        title: 'Inputs',
        headers: ['Item', 'Current Month', 'Previous Month', 'Difference (Current - Previous)'],
        rows: [
            ['Months', currentMonth, previousMonth, 'NA'],
            ['Days in the month', currentDays.toString(), previousDays.toString(), 'NA'],
            ['Eth apr', currentEthApr.toString(), previousEthApr.toString(), calculateDifference(currentEthApr, previousEthApr)],
            ['Ssv eth', currentSsvEth.toString(), previousSsvEth.toString(), calculateDifference(currentSsvEth, previousSsvEth)],
            ['Network fee', currentNetworkFee.toString(), previousNetworkFee.toString(), calculateDifference(currentNetworkFee, previousNetworkFee)],
            ['Apr boost', currentAprBoost.toString(), previousAprBoost.toString(), calculateDifference(currentAprBoost, previousAprBoost)],
            ['Reward tier', currentRewardTier.toFixed(18), previousRewardTier.toFixed(18), calculateDifference(currentRewardTier, previousRewardTier)]
        ]
    };
}

function generateValidatorRedirectsSection(): TableSection {
    const currentMonth = process.env.CURRENT_MONTH!;
    const previousMonth = process.env.PREVIOUS_MONTH!;

    const currentStats = loadRedirectStats('current');
    const previousStats = loadRedirectStats('previous');

    const currentValidatorAmountThatDidntRedirect = currentStats.declaredValidatorRedirects - currentStats.validatorsWithValidatorRedirects;
    const previousValidatorAmountThatDidntRedirect = previousStats.declaredValidatorRedirects - previousStats.validatorsWithValidatorRedirects;

    return {
        title: 'Validator Redirects',
        headers: ['Item', 'Current Month', 'Previous Month', 'Difference (Current - Previous)'],
        rows: [
            ['Months', currentMonth, previousMonth, 'NA'],
            ['Total validators on the network', formatNumber(currentStats.totalValidators), formatNumber(previousStats.totalValidators), calculateDifference(currentStats.totalValidators, previousStats.totalValidators)],
            ['Validator amount in validator-redirects', formatNumber(currentStats.declaredValidatorRedirects), formatNumber(previousStats.declaredValidatorRedirects), calculateDifference(currentStats.declaredValidatorRedirects, previousStats.declaredValidatorRedirects)],
            ['Validator amount that redirected', formatNumber(currentStats.validatorsWithValidatorRedirects), formatNumber(previousStats.validatorsWithValidatorRedirects), calculateDifference(currentStats.validatorsWithValidatorRedirects, previousStats.validatorsWithValidatorRedirects)],
            ['Validator amount that didnt redirect from validator-redirects', formatNumber(currentValidatorAmountThatDidntRedirect), formatNumber(previousValidatorAmountThatDidntRedirect), calculateDifference(currentValidatorAmountThatDidntRedirect, previousValidatorAmountThatDidntRedirect)],
            ['Validator amount redirected by owner-redirects', formatNumber(currentStats.validatorsWithOwnerRedirects), formatNumber(previousStats.validatorsWithOwnerRedirects), calculateDifference(currentStats.validatorsWithOwnerRedirects, previousStats.validatorsWithOwnerRedirects)],
            ['Total Validator amount with no redirects', formatNumber(currentStats.validatorsWithNoRedirects), formatNumber(previousStats.validatorsWithNoRedirects), calculateDifference(currentStats.validatorsWithNoRedirects, previousStats.validatorsWithNoRedirects)]
        ]
    };
}

function generateOwnerRedirectsSection(): TableSection {
    const currentMonth = process.env.CURRENT_MONTH!;
    const previousMonth = process.env.PREVIOUS_MONTH!;

    const currentStats = loadRedirectStats('current');
    const previousStats = loadRedirectStats('previous');

    return {
        title: 'Owner Redirects',
        headers: ['Item', 'Current Month', 'Previous Month', 'Difference (Current - Previous)'],
        rows: [
            ['Months', currentMonth, previousMonth, 'NA'],
            ['Total Owner Addresses', formatNumber(currentStats.totalOwnerAddresses), formatNumber(previousStats.totalOwnerAddresses), calculateDifference(currentStats.totalOwnerAddresses, previousStats.totalOwnerAddresses)],
            ['Total Recipient Addresses', formatNumber(currentStats.totalRecipientAddresses), formatNumber(previousStats.totalRecipientAddresses), calculateDifference(currentStats.totalRecipientAddresses, previousStats.totalRecipientAddresses)],
            ['Owner amount in owner-redirects', formatNumber(currentStats.declaredOwnerRedirects), formatNumber(previousStats.declaredOwnerRedirects), calculateDifference(currentStats.declaredOwnerRedirects, previousStats.declaredOwnerRedirects)],
            ['Owner amount that redirected', formatNumber(currentStats.redirectedOwners), formatNumber(previousStats.redirectedOwners), calculateDifference(currentStats.redirectedOwners, previousStats.redirectedOwners)],
            ['Total owner amount with no redirects', formatNumber(currentStats.ownersWithNoRedirects), formatNumber(previousStats.ownersWithNoRedirects), calculateDifference(currentStats.ownersWithNoRedirects, previousStats.ownersWithNoRedirects)]
        ]
    };
}

function generateDistributionRateSection(): TableSection {
    const currentMonth = process.env.CURRENT_MONTH!;
    const previousMonth = process.env.PREVIOUS_MONTH!;

    const currentDays = getDaysInMonth(currentMonth);
    const previousDays = getDaysInMonth(previousMonth);

    const currentValidators = loadValidatorData('current');
    const previousValidators = loadValidatorData('previous');

    const currentAprBoostTiers: AprBoostTier[] = JSON.parse(process.env.CURRENT_APR_BOOST!);
    const previousAprBoostTiers: AprBoostTier[] = JSON.parse(process.env.PREVIOUS_APR_BOOST!);

    const currentAverageRatio = calculateTotalEffectiveBalancePerDayAverage(currentValidators);
    const previousAverageRatio = calculateTotalEffectiveBalancePerDayAverage(previousValidators);

    const currentAprBoost = determineAprBoost(currentAverageRatio, currentAprBoostTiers).toNumber();
    const previousAprBoost = determineAprBoost(previousAverageRatio, previousAprBoostTiers).toNumber();

    const currentSsvEth = parseFloat(process.env.CURRENT_SSV_ETH!);
    const previousSsvEth = parseFloat(process.env.PREVIOUS_SSV_ETH!);

    // Calculate effective balance: cumulative active effective balance / days in the month
    const currentActiveBalance = currentValidators.reduce((sum, v) => sum + v.TotalActiveEffectiveBalance, 0);
    const previousActiveBalance = previousValidators.reduce((sum, v) => sum + v.TotalActiveEffectiveBalance, 0);
    const currentEffectiveBalance = parseFloat((currentActiveBalance / currentDays).toFixed(5));
    const previousEffectiveBalance = parseFloat((previousActiveBalance / previousDays).toFixed(5));

    const currentEthApr = parseFloat(process.env.CURRENT_ETH_APR!);
    const previousEthApr = parseFloat(process.env.PREVIOUS_ETH_APR!);

    // New incentives calculation: eth apr * effective balance * apr boost * (1 / eth ssv) / 365 * days in the month
    const currentIncentives = parseFloat((currentEthApr * currentEffectiveBalance * currentAprBoost * (1 / currentSsvEth) / 365 * currentDays).toFixed(5));
    const previousIncentives = parseFloat((previousEthApr * previousEffectiveBalance * previousAprBoost * (1 / previousSsvEth) / 365 * previousDays).toFixed(5));

    // Calculate ratios for results
    const daysRatio = ((currentDays / previousDays) - 1) * 100;
    const aprBoostRatio = ((currentAprBoost / previousAprBoost) - 1) * 100;
    const ssvEthRatio = ((currentSsvEth / previousSsvEth) - 1) * 100;
    const effectiveBalanceRatio = ((currentEffectiveBalance / previousEffectiveBalance) - 1) * 100;
    const ethAprRatio = ((currentEthApr / previousEthApr) - 1) * 100;
    const incentivesRatio = ((currentIncentives - previousIncentives) / previousIncentives) * 100;

    // New reward math: eth apr result * effective balance result * apr boost result * ((1 / eth ssv result) / 1 * days in month result) - 1
    const rewardResult = ((1 + ethAprRatio / 100) * (1 + effectiveBalanceRatio / 100) * (1 + aprBoostRatio / 100) * (1 / (1 + ssvEthRatio / 100)) * (1 + daysRatio / 100) - 1) * 100;

    // Check if results match incentives (within a small tolerance for floating point precision)
    const tolerance = 0.001; // 0.001% tolerance
    const resultsMatch = Math.abs(rewardResult - incentivesRatio) < tolerance ? 'MATCHES' : 'DOESNT MATCH';

    return {
        title: 'Distribution Rate',
        headers: ['Item', 'Current Month', 'Previous Month', 'Result'],
        rows: [
            ['Month', currentMonth, previousMonth, 'NA'],
            ['Days of month', currentDays.toString(), previousDays.toString(), formatPercentage(daysRatio)],
            ['APR Boost', formatPercentage(currentAprBoost * 100), formatPercentage(previousAprBoost * 100), formatPercentage(aprBoostRatio)],
            ['SSV ETH', currentSsvEth.toString(), previousSsvEth.toString(), formatPercentage(ssvEthRatio)],
            ['Effective balance', formatNumber(currentEffectiveBalance.toFixed(5)), formatNumber(previousEffectiveBalance.toFixed(5)), formatPercentage(effectiveBalanceRatio)],
            ['ETH APR', formatPercentage(currentEthApr * 100, 5), formatPercentage(previousEthApr * 100, 5), formatPercentage(ethAprRatio)],
            ['Incentives', currentIncentives.toFixed(5), previousIncentives.toFixed(5), formatPercentage(incentivesRatio)],
            ['Reward', 'NA', 'NA', formatPercentage(rewardResult, 3)],
            ['Results match incentives', 'NA', 'NA', resultsMatch]
        ]
    };
}


function displayTable(section: TableSection): void {
    console.log(`\n📊 ${section.title.toUpperCase()}`);
    console.log('='.repeat(section.title.length + 4));

    const colWidths = section.headers.map((_, i) =>
        Math.max(section.headers[i].length, ...section.rows.map(row => row[i].replace(/\*\*/g, '').length))
    );

    // Display headers
    const headerRow = section.headers.map((header, i) => header.padEnd(colWidths[i])).join(' | ');
    console.log(`| ${headerRow} |`);

    // Display separator
    const separator = colWidths.map(width => '─'.repeat(width)).join(' | ');
    console.log(`| ${separator} |`);

    // Display rows
    section.rows.forEach(row => {
        const displayRow = row.map((cell, i) => cell.replace(/\*\*/g, '').padEnd(colWidths[i])).join(' | ');
        console.log(`| ${displayRow} |`);
    });
}

function generateCSV(sections: TableSection[]): string {
    let csv = '';

    sections.forEach((section, index) => {
        if (index > 0) csv += '\n\n';

        csv += `${section.title}\n`;
        csv += section.headers.join(',') + '\n';
        section.rows.forEach(row => {
            csv += row.map(cell => `"${cell.replace(/"/g, '""')}"`).join(',') + '\n';
        });
    });

    return csv;
}

async function main(): Promise<void> {
    try {
        const sections: TableSection[] = [
            generateOverallSection(),
            generateInputsSection(),
            generateValidatorRedirectsSection(),
            generateOwnerRedirectsSection(),
            generateDistributionRateSection()
        ];

        // Display all tables
        sections.forEach(section => {
            displayTable(section);
        });

        // Generate and save CSV
        const csvContent = generateCSV(sections);
        const outputPath = join(__dirname, `results-${process.env.CURRENT_MONTH!}.csv`);
        writeFileSync(outputPath, csvContent);

        console.log(`\n✅ Monthly statistics complete! CSV saved to: ${outputPath}`);

    } catch (error) {
        console.error('❌ Error generating monthly statistics:', error);
        process.exit(1);
    }
}

if (require.main === module) {
    main().catch(error => {
        console.error('❌ Unexpected error:', error);
        process.exit(1);
    });
}