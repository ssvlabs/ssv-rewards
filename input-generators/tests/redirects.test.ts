import { readFileSync } from 'fs';
import { join } from 'path';
import * as dotenv from 'dotenv';

dotenv.config();

interface ValidatorRecord {
    RecipientAddress: string;
    OwnerAddress: string;
    PublicKey: string;
    ActiveDays: number;
    RegisteredDays: number;
    TotalActiveEffectiveBalance: string;
    TotalRegisteredEffectiveBalance: string;
    FeeDeduction: string;
    Reward: string;
}

interface RecipientRecord {
    RecipientAddress: string;
    TotalFeeDeduction: string;
    TotalReward: string;
}

interface OwnerRedirect {
    from: string;
    to: string;
}

interface ValidatorRedirect {
    from: string; // validator public key
    to: string;   // recipient address
}

function parseCSV<T>(filePath: string, parser: (row: string[]) => T): T[] {
    const content = readFileSync(filePath, 'utf-8');
    const lines = content.trim().split('\n');
    const headers = lines[0].split(/[,\t]/);
    
    return lines.slice(1).map(line => {
        const values = line.split(/[,\t]/);
        return parser(values);
    });
}

function loadByValidatorData(): ValidatorRecord[] {
    const filePath = join(__dirname, '../data/current/by-validator.csv');
    return parseCSV(filePath, (values) => ({
        RecipientAddress: values[0],
        OwnerAddress: values[1], 
        PublicKey: values[2],
        ActiveDays: parseInt(values[3]),
        RegisteredDays: parseInt(values[4]),
        TotalActiveEffectiveBalance: values[5],
        TotalRegisteredEffectiveBalance: values[6],
        FeeDeduction: values[7],
        Reward: values[8]
    }));
}

function loadByRecipientData(): RecipientRecord[] {
    const filePath = join(__dirname, '../data/current/by-recipient.csv');
    return parseCSV(filePath, (values) => ({
        RecipientAddress: values[0],
        TotalFeeDeduction: values[1],
        TotalReward: values[2]
    }));
}

function loadOwnerRedirects(): OwnerRedirect[] {
    const filePath = join(__dirname, '../data/current/owner-redirects.csv');
    return parseCSV(filePath, (values) => ({
        from: values[0],
        to: values[1]
    }));
}

function loadValidatorRedirects(): ValidatorRedirect[] {
    const currentMonth = process.env.CURRENT_MONTH;
    if (!currentMonth) {
        throw new Error('CURRENT_MONTH environment variable not set');
    }
    
    const filePath = join(__dirname, `../data/current/validator-redirects-${currentMonth}.csv`);
    return parseCSV(filePath, (values) => ({
        from: values[0], // validator public key
        to: values[1]    // recipient address
    }));
}

describe('Redirects Validation', () => {
    test('should validate all owner and validator redirects are correctly applied', () => {
        // Load all data silently
        const validatorData = loadByValidatorData();
        const recipientData = loadByRecipientData();
        const ownerRedirects = loadOwnerRedirects();
        const validatorRedirects = loadValidatorRedirects();
        
        // Helper function to normalize addresses (remove 0x and lowercase)
        const normalizeAddress = (addr: string): string => {
            return addr.toLowerCase().replace(/^0x/, '');
        };
        
        // Create lookup maps for redirects
        const ownerRedirectMap = new Map<string, string>();
        ownerRedirects.forEach(redirect => {
            ownerRedirectMap.set(normalizeAddress(redirect.from), normalizeAddress(redirect.to));
        });
        
        const validatorRedirectMap = new Map<string, string>();
        validatorRedirects.forEach(redirect => {
            validatorRedirectMap.set(normalizeAddress(redirect.from), normalizeAddress(redirect.to));
        });
        
        const ALLOWED_ORPHANED_RECIPIENT = 'b35096b074fdb9bbac63e3adae0bbde512b2e6b6';
        
        let totalValidated = 0;
        let correctRedirects = 0;
        const errors: string[] = [];
        
        // Statistics tracking
        let validatorsWithValidatorRedirects = 0;
        let validatorsWithOwnerRedirects = 0;
        let validatorsWithNoRedirects = 0;
        
        const ownerRedirectsActuallyUsed = new Set<string>();
        const validatorRedirectsActuallyUsed = new Set<string>();
        
        // Check each validator
        for (const validator of validatorData) {
            totalValidated++;
            
            const ownerAddress = normalizeAddress(validator.OwnerAddress);
            const recipientAddress = normalizeAddress(validator.RecipientAddress);
            const publicKey = normalizeAddress(validator.PublicKey);
            
            let expectedRecipient = ownerAddress; // Default: recipient should match owner
            let redirectType = 'none';
            
            // Check if there's an owner redirect
            if (ownerRedirectMap.has(ownerAddress)) {
                expectedRecipient = ownerRedirectMap.get(ownerAddress)!;
                redirectType = 'owner';
            }
            
            // Check if there's a validator redirect (this overrides owner redirect)
            if (validatorRedirectMap.has(publicKey)) {
                expectedRecipient = validatorRedirectMap.get(publicKey)!;
                redirectType = 'validator';
                validatorRedirectsActuallyUsed.add(publicKey);
                validatorsWithValidatorRedirects++;
            } else if (ownerRedirectMap.has(ownerAddress)) {
                ownerRedirectsActuallyUsed.add(ownerAddress);
                validatorsWithOwnerRedirects++;
            } else {
                validatorsWithNoRedirects++;
            }
            
            // Validate the recipient address
            if (recipientAddress === expectedRecipient) {
                correctRedirects++;
            } else {
                const errorMsg = `❌ REDIRECT MISMATCH - Validator ${publicKey}:
   Expected: ${expectedRecipient} | Actual: ${recipientAddress} | Type: ${redirectType}`;
                errors.push(errorMsg);
                process.stdout.write(errorMsg + '\n');
            }
        }
        
        // Validate recipient consistency
        const validatorRecipients = new Set(validatorData.map(v => normalizeAddress(v.RecipientAddress)));
        const recipientAddresses = new Set(recipientData.map(r => normalizeAddress(r.RecipientAddress)));
        
        // Check for recipients in by-recipient.csv that don't exist in by-validator.csv (except allowed one)
        for (const recipientAddr of recipientAddresses) {
            if (!validatorRecipients.has(recipientAddr)) {
                if (recipientAddr !== ALLOWED_ORPHANED_RECIPIENT) {
                    const errorMsg = `❌ RECIPIENT CONSISTENCY FAILURE - ${recipientAddr} exists in by-recipient.csv but has no validators in by-validator.csv`;
                    errors.push(errorMsg);
                    process.stdout.write(errorMsg + '\n');
                }
            }
        }
        
        // Check for recipients in by-validator.csv that don't exist in by-recipient.csv
        for (const recipientAddr of validatorRecipients) {
            if (!recipientAddresses.has(recipientAddr)) {
                const errorMsg = `❌ RECIPIENT CONSISTENCY FAILURE - ${recipientAddr} has validators but missing from by-recipient.csv`;
                errors.push(errorMsg);
                process.stdout.write(errorMsg + '\n');
            }
        }
        
        // Calculate additional statistics
        const usedOwners = new Set(validatorData.map(v => normalizeAddress(v.OwnerAddress)));
        const usedValidators = new Set(validatorData.map(v => normalizeAddress(v.PublicKey)));
        
        const ownerRedirectsDeclared = ownerRedirects.length;
        const validatorRedirectsDeclared = validatorRedirects.length;
        
        const ownerRedirectsUnused = ownerRedirects.filter(redirect => 
            !usedOwners.has(normalizeAddress(redirect.from))
        ).length;
        
        const validatorRedirectsUnused = validatorRedirects.filter(redirect => 
            !usedValidators.has(normalizeAddress(redirect.from))
        ).length;
        
        const ownersActuallyRedirected = ownerRedirectsActuallyUsed.size;
        const validatorsActuallyRedirected = validatorRedirectsActuallyUsed.size;
        
        // Calculate total unique addresses
        const totalOwnerAddresses = usedOwners.size;
        const totalRecipientAddresses = validatorRecipients.size;
        
        // Display comprehensive statistics table
        if (errors.length === 0) {
            process.stdout.write('\n✅ ALL REDIRECT VALIDATIONS PASSED!\n');
        } else {
            process.stdout.write(`\n❌ VALIDATION FAILED - ${errors.length} errors found\n`);
        }
        
        process.stdout.write('\n📊 REDIRECT STATISTICS TABLE\n');
        process.stdout.write(' ═══════════════════════════════════════════════════════\n');
        process.stdout.write('│ VALIDATORS                                 │ Count    │\n');
        process.stdout.write('├────────────────────────────────────────────┼──────────┤\n');
        process.stdout.write(`│ Total Validators                           │ ${totalValidated.toString().padStart(8)} │\n`);
        process.stdout.write(`│ Declared Validator Redirects               │ ${validatorRedirectsDeclared.toString().padStart(8)} │\n`);
        process.stdout.write(`│ Validator Redirects                        │ ${validatorsWithValidatorRedirects.toString().padStart(8)} │\n`);
        process.stdout.write(`│ Owner Redirects                            │ ${validatorsWithOwnerRedirects.toString().padStart(8)} │\n`);
        process.stdout.write(`│ Validators with No Redirects               │ ${validatorsWithNoRedirects.toString().padStart(8)} │\n`);
        process.stdout.write('├────────────────────────────────────────────┼──────────┤\n');
        process.stdout.write('│ OWNERS                                     │ Count    │\n');
        process.stdout.write('├────────────────────────────────────────────┼──────────┤\n');
        process.stdout.write(`│ Total Owner Addresses                      │ ${totalOwnerAddresses.toString().padStart(8)} │\n`);
        process.stdout.write(`│ Total Recipient Addresses                  │ ${totalRecipientAddresses.toString().padStart(8)} │\n`);
        process.stdout.write(`│ Declared Owner Redirects                   │ ${ownerRedirectsDeclared.toString().padStart(8)} │\n`);
        process.stdout.write(`│ Redirected Owners                          │ ${ownersActuallyRedirected.toString().padStart(8)} │\n`);
        process.stdout.write(`│ Owners with No Redirects                   │ ${ownerRedirectsUnused.toString().padStart(8)} │\n`);
        process.stdout.write('└────────────────────────────────────────────┴──────────┘\n');
        
        // Test assertions
        expect(correctRedirects).toBe(totalValidated);
        expect(errors.length).toBe(0);
    });
});