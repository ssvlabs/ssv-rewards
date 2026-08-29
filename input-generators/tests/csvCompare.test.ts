import fs from "fs";
import path from "path";
import csv from "csv-parser";

const DECIMALS = 18;
const TARGET_RECIPIENT = "b35096b074fdb9bbac63e3adae0bbde512b2e6b6";

function pow10n(exp: number): bigint {
  let r = BigInt(1);
  const ten = BigInt(10);
  for (let i = 0; i < exp; i++) r *= ten;
  return r;
}
const SCALE = pow10n(DECIMALS);

interface RowAtoms {
  recipientAddress: string;
  feeDeductionAtoms: bigint;
  rewardAtoms: bigint;
}

function parseUnits18(val: string): bigint {
  let s = (val ?? "").trim().replace(/,/g, "");
  if (s === "" || s.toLowerCase() === "nan") return BigInt(0);

  let neg = false;
  if (s[0] === "-") { neg = true; s = s.slice(1); }
  else if (s[0] === "+") { s = s.slice(1); }

  const [iPartRaw, fPartRaw = ""] = s.split(".");
  const iPart = iPartRaw || "0";
  const fPadded = (fPartRaw + "0".repeat(DECIMALS)).slice(0, DECIMALS);

  if (!/^\d+$/.test(iPart) || (fPadded && !/^\d+$/.test(fPadded))) {
    throw new Error(`Invalid decimal: ${val}`);
  }

  const atoms = BigInt(iPart) * SCALE + (fPadded ? BigInt(fPadded) : BigInt(0));
  return neg ? -atoms : atoms;
}

function formatUnits18(atoms: bigint): string {
  const neg = atoms < BigInt(0);
  let s = (neg ? -atoms : atoms).toString().padStart(DECIMALS + 1, "0");
  const i = s.slice(0, -DECIMALS) || "0";
  let f = s.slice(-DECIMALS);
  f = f.replace(/0+$/, "");
  return (neg ? "-" : "") + (f ? `${i}.${f}` : i);
}

function cleanHeader(h: string) {
  return (h || "").trim().replace(/^\uFEFF/, "");
}

function normalizeAddr(s: string) {
  const x = (s || "").trim().toLowerCase();
  return x.startsWith("0x") ? x.slice(2) : x;
}

function parseAtomField(data: any, keyA: string, keyB: string): bigint {
  const raw = (data[keyA] ?? data[keyB] ?? "0").toString();
  try { return parseUnits18(raw); } catch { return BigInt(0); }
}

async function readCSVAtoms(filePath: string): Promise<RowAtoms[]> {
  return new Promise((resolve, reject) => {
    const rows: RowAtoms[] = [];
    fs.createReadStream(filePath)
      .pipe(
        csv({
          separator: "\t",
          mapHeaders: ({ header }) => cleanHeader(header),
        })
      )
      .on("data", (data) => {
        const recipient =
          (data.RecipientAddress || data.recipientAddress || "").toString().trim();
        if (!recipient) return;
        rows.push({
          recipientAddress: normalizeAddr(recipient),
          feeDeductionAtoms: parseAtomField(data, "FeeDeduction", "feeDeduction"),
          rewardAtoms: parseAtomField(data, "Reward", "reward"),
        });
      })
      .on("end", () => resolve(rows))
      .on("error", reject);
  });
}

describe('Fee Reward Consistency Tests', () => {
  test('by-validator aggregated data should match by-recipient data for each unique recipient', async () => {
    const byRecipientPath = path.resolve("./data/current/by-recipient.csv");
    const byValidatorPath = path.resolve("./data/current/by-validator.csv");

    const byRecipient = await readCSVAtoms(byRecipientPath);
    const byValidator = await readCSVAtoms(byValidatorPath);

    // Aggregate by-validator data by recipient address
    const validatorAggregated: Record<string, { feeDeductionAtoms: bigint; rewardAtoms: bigint }> = {};
    for (const row of byValidator) {
      const key = row.recipientAddress;
      if (!validatorAggregated[key]) {
        validatorAggregated[key] = { feeDeductionAtoms: BigInt(0), rewardAtoms: BigInt(0) };
      }
      validatorAggregated[key].feeDeductionAtoms += row.feeDeductionAtoms;
      validatorAggregated[key].rewardAtoms += row.rewardAtoms;
    }

    // Create map for by-recipient data
    const recipientMap: Record<string, { feeDeductionAtoms: bigint; rewardAtoms: bigint }> = {};
    for (const row of byRecipient) {
      recipientMap[row.recipientAddress] = {
        feeDeductionAtoms: row.feeDeductionAtoms,
        rewardAtoms: row.rewardAtoms,
      };
    }

    // Get all unique recipients
    const allRecipients = new Set([
      ...Object.keys(validatorAggregated),
      ...Object.keys(recipientMap),
    ]);

    process.stdout.write(`\nTesting ${allRecipients.size} unique recipients...\n`);

    let mismatches = 0;
    for (const recipient of allRecipients) {
      const validator = validatorAggregated[recipient];
      const recipientData = recipientMap[recipient];

      if (!validator) {
        // Special case: TARGET_RECIPIENT is allowed to exist only in by-recipient
        if (recipient === TARGET_RECIPIENT) continue;

        process.stdout.write(`❌ Recipient ${recipient} exists in by-recipient but not in by-validator\n`);
        mismatches++;
        continue;
      }
      if (!recipientData) {
        process.stdout.write(`❌ Recipient ${recipient} exists in by-validator but not in by-recipient\n`);
        mismatches++;
        continue;
      }

      // Check fee deductions match
      if (validator.feeDeductionAtoms !== recipientData.feeDeductionAtoms) {
        process.stdout.write(`❌ Fee deduction mismatch for ${recipient}:\n`);
        process.stdout.write(`   by-validator: ${formatUnits18(validator.feeDeductionAtoms)}\n`);
        process.stdout.write(`   by-recipient: ${formatUnits18(recipientData.feeDeductionAtoms)}\n`);
        mismatches++;
      }

      // Check rewards match
      if (validator.rewardAtoms !== recipientData.rewardAtoms) {
        process.stdout.write(`❌ Reward mismatch for ${recipient}:\n`);
        process.stdout.write(`   by-validator: ${formatUnits18(validator.rewardAtoms)}\n`);
        process.stdout.write(`   by-recipient: ${formatUnits18(recipientData.rewardAtoms)}\n`);
        mismatches++;
      }
    }

    if (mismatches === 0) {
      process.stdout.write(`✅ All ${allRecipients.size} recipients match between by-validator and by-recipient\n`);
    }

    expect(mismatches).toBe(0);
  }, 30000);

  test('total fee deductions should equal b35096b074fdb9bbac63e3adae0bbde512b2e6b6 reward', async () => {
    const byRecipientPath = path.resolve("./data/current/by-recipient.csv");
    const byValidatorPath = path.resolve("./data/current/by-validator.csv");

    const byRecipient = await readCSVAtoms(byRecipientPath);
    const byValidator = await readCSVAtoms(byValidatorPath);

    // Sum all fee deductions from by-validator
    let totalValidatorFeeDeductions = BigInt(0);
    for (const row of byValidator) {
      totalValidatorFeeDeductions += row.feeDeductionAtoms;
    }

    // Sum all fee deductions from by-recipient
    let totalRecipientFeeDeductions = BigInt(0);
    for (const row of byRecipient) {
      totalRecipientFeeDeductions += row.feeDeductionAtoms;
    }

    // Find target recipient reward
    const targetRecipient = byRecipient.find(
      row => row.recipientAddress === TARGET_RECIPIENT
    );

    expect(targetRecipient).toBeDefined();

    const targetReward = targetRecipient!.rewardAtoms;

    let feeDeductionMismatches = 0;

    // Check if by-validator fee deductions match target recipient reward
    if (totalValidatorFeeDeductions !== targetReward) {
      process.stdout.write(`❌ By-validator fee deductions don't match target recipient reward:\n`);
      process.stdout.write(`   Total fee deductions (by-validator): ${formatUnits18(totalValidatorFeeDeductions)}\n`);
      process.stdout.write(`   Target recipient reward: ${formatUnits18(targetReward)}\n`);
      feeDeductionMismatches++;
    }

    // Check if by-recipient fee deductions match target recipient reward
    if (totalRecipientFeeDeductions !== targetReward) {
      process.stdout.write(`❌ By-recipient fee deductions don't match target recipient reward:\n`);
      process.stdout.write(`   Total fee deductions (by-recipient): ${formatUnits18(totalRecipientFeeDeductions)}\n`);
      process.stdout.write(`   Target recipient reward: ${formatUnits18(targetReward)}\n`);
      feeDeductionMismatches++;
    }

    if (feeDeductionMismatches === 0) {
      process.stdout.write(`✅ All fee deductions match target recipient ${TARGET_RECIPIENT} reward (${formatUnits18(targetReward)})\n`);
    }

    expect(feeDeductionMismatches).toBe(0);
  }, 30000);
});