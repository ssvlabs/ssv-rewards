import dotenv from 'dotenv';

dotenv.config();

const CURRENT_MONTH = process.env.CURRENT_MONTH;
const BEACON_CHAIN_GENESIS_DATE = new Date('2020-12-01'); // Ethereum 2.0 genesis date

interface EthStoreData {
  day: number;
  apr: number;
  dayStart: string;
}

interface BeaconApiData {
  apr: number;
  day: number;
  day_start: string;
  day_end: string;
  total_rewards_wei: string;
  consensus_rewards_sum_wei: string;
  tx_fees_sum_wei: string;
  [key: string]: any;
}

interface BeaconApiResponse {
  status: string;
  data: BeaconApiData;
}

interface MonthlyStats {
  highestApr: number;
  lowestApr: number;
  averageApr: number;
  highestAprDay: string;
  lowestAprDay: string;
  dateRange: string;
  dayRange: string;
  totalDays: number;
}

function calculateDayFromDate(date: Date): number {
  const diffTime = date.getTime() - BEACON_CHAIN_GENESIS_DATE.getTime();
  const diffDays = Math.floor(diffTime / (1000 * 60 * 60 * 24));
  return diffDays + 1; // Day 1 starts from genesis
}

function getMonthDateRange(yearMonth: string): { startDate: Date; endDate: Date } {
  const [year, month] = yearMonth.split('-').map(Number);
  const startDate = new Date(year, month - 1, 1); // month is 0-indexed
  const endDate = new Date(year, month, 0); // Last day of the month

  return { startDate, endDate };
}

async function fetchEthStoreData(day: number, expectedDate: Date): Promise<EthStoreData | null> {
  try {
    const response = await fetch(`https://beaconcha.in/api/v1/ethstore/${day}`);

    if (!response.ok) {
      console.warn(`⚠️  Failed to fetch data for day ${day}: ${response.status}`);
      return null;
    }

    const apiResponse = await response.json() as BeaconApiResponse;

    if (apiResponse.status !== 'OK' || !apiResponse.data) {
      console.warn(`⚠️  API returned non-OK status for day ${day}: ${apiResponse.status}`);
      return null;
    }

    const data = apiResponse.data;

    // Validate that the day_start matches our expected date
    const dayStartDate = new Date(data.day_start);
    const expectedDateStr = expectedDate.toISOString().split('T')[0];
    const dayStartDateStr = dayStartDate.toISOString().split('T')[0];

    if (expectedDateStr !== dayStartDateStr) {
      console.warn(`⚠️  Day ${day} start date mismatch: expected ${expectedDateStr}, got ${dayStartDateStr}`);
      return null;
    }

    // console.log(`✅ Day ${day}: ${dayStartDateStr} - APR: ${(data.apr * 100).toFixed(4)}%`);

    return {
      day: data.day,
      apr: data.apr,
      dayStart: data.day_start
    };
  } catch (error) {
    console.warn(`⚠️  Error fetching data for day ${day}:`, error);
    return null;
  }
}

function formatDate(date: Date): string {
  return date.toISOString().split('T')[0]; // YYYY-MM-DD format
}

function displayResults(stats: MonthlyStats, dailyData: EthStoreData[]): void {
  console.log('\n📊 Monthly ETH Store APR Statistics');
  console.log('=====================================');

  const table = [
    ['Item', 'Value'],
    ['─'.repeat(25), '─'.repeat(30)],
    ['Highest APR', `${(stats.highestApr * 100).toFixed(6)}% (${stats.highestAprDay})`],
    ['Lowest APR', `${(stats.lowestApr * 100).toFixed(6)}% (${stats.lowestAprDay})`],
    ['Average APR', `${(stats.averageApr * 100).toFixed(6)}%`],
    ['Date Range', stats.dateRange],
    ['Day Range', stats.dayRange],
    ['Total Days Processed', stats.totalDays.toString()],
    ['Successful API Calls', dailyData.filter(d => d !== null).length.toString()],
    ['Eth APR Input', `${stats.averageApr.toFixed(5)}`]
  ];

  // Find the longest strings in each column for padding
  const col1Width = Math.max(...table.map(row => row[0].length));
  const col2Width = Math.max(...table.map(row => row[1].length));

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

  console.log('\n✅ Analysis complete!');
}

async function main(): Promise<void> {
  if (!CURRENT_MONTH) {
    console.error('❌ CURRENT_MONTH environment variable is not set');
    process.exit(1);
  }

  console.log(`🔍 Analyzing ETH store APR data for ${CURRENT_MONTH}`);

  const { startDate, endDate } = getMonthDateRange(CURRENT_MONTH);
  const startDay = calculateDayFromDate(startDate);
  const endDay = calculateDayFromDate(endDate);

  console.log(`📅 Date range: ${formatDate(startDate)} to ${formatDate(endDate)}`);
  console.log(`📊 Day range: ${startDay} to ${endDay}`);
  console.log(`🌐 Fetching data from beaconcha.in API`);

  const dailyData: EthStoreData[] = [];
  const aprValues: number[] = [];

  // Fetch data for each day in the month
  for (let day = startDay; day <= endDay; day++) {
    const currentDate = new Date(startDate);
    currentDate.setDate(currentDate.getDate() + (day - startDay));

    // The API day starts 1 day ahead of our calculation, so adjust expected date
    const expectedApiDate = new Date(currentDate);
    expectedApiDate.setDate(expectedApiDate.getDate() + 1);

    const data = await fetchEthStoreData(day, expectedApiDate);
    if (data && data.apr !== undefined && data.apr > 0) {
      dailyData.push(data);
      aprValues.push(data.apr);
    }

    // Add 1.5 second delay to avoid rate limiting
    await new Promise(resolve => setTimeout(resolve, 1500));
  }

  if (aprValues.length === 0) {
    console.error('❌ No valid APR data retrieved from API');
    process.exit(1);
  }

  // Find highest and lowest APR with their dates
  const highestApr = Math.max(...aprValues);
  const lowestApr = Math.min(...aprValues);

  const highestAprData = dailyData.find(d => d.apr === highestApr);
  const lowestAprData = dailyData.find(d => d.apr === lowestApr);

  const highestAprDay = highestAprData ? new Date(highestAprData.dayStart).toISOString().split('T')[0] : 'Unknown';
  const lowestAprDay = lowestAprData ? new Date(lowestAprData.dayStart).toISOString().split('T')[0] : 'Unknown';

  // Use actual first and last dates from API responses for correct date range
  const actualStartDate = dailyData.length > 0 ? new Date(dailyData[0].dayStart).toISOString().split('T')[0] : formatDate(startDate);
  const actualEndDate = dailyData.length > 0 ? new Date(dailyData[dailyData.length - 1].dayStart).toISOString().split('T')[0] : formatDate(endDate);

  // Calculate statistics
  const stats: MonthlyStats = {
    highestApr,
    lowestApr,
    averageApr: aprValues.reduce((sum, apr) => sum + apr, 0) / aprValues.length,
    highestAprDay,
    lowestAprDay,
    dateRange: `${actualStartDate} - ${actualEndDate}`,
    dayRange: `${startDay}-${endDay}`,
    totalDays: endDay - startDay + 1
  };

  displayResults(stats, dailyData);
}

if (require.main === module) {
  main().catch(error => {
    console.error('❌ Error:', error);
    process.exit(1);
  });
}