import dotenv from 'dotenv';

dotenv.config();

const CURRENT_MONTH = process.env.CURRENT_MONTH;

interface BinanceKline {
  0: number;  // Open time
  1: string;  // Open price
  2: string;  // High price
  3: string;  // Low price
  4: string;  // Close price
  5: string;  // Volume
  6: number;  // Close time
  7: string;  // Quote asset volume
  8: number;  // Number of trades
  9: string;  // Taker buy base asset volume
  10: string; // Taker buy quote asset volume
  11: string; // Ignore
}

interface DailyCandle {
  date: string;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
}

interface MonthlyStats {
  ssvUsdtAverage: number;
  ssvUsdtAverageOriginal: number;
  ethUsdtAverage: number;
  ssvEthRatio: number;
  highestRatio: number;
  lowestRatio: number;
  highestRatioDay: string;
  lowestRatioDay: string;
  highestSsvPrice: number;
  lowestSsvPrice: number;
  highestSsvPriceDay: string;
  lowestSsvPriceDay: string;
  highestEthPrice: number;
  lowestEthPrice: number;
  highestEthPriceDay: string;
  lowestEthPriceDay: string;
  dateRange: string;
  totalDays: number;
  ssvCandles: DailyCandle[];
  ethCandles: DailyCandle[];
}

function getMonthDateRange(yearMonth: string): { startDate: Date; endDate: Date } {
  const [year, month] = yearMonth.split('-').map(Number);
  const startDate = new Date(year, month - 1, 1); // month is 0-indexed
  const endDate = new Date(year, month, 0); // Last day of the month
  
  return { startDate, endDate };
}

function formatDate(date: Date): string {
  return date.toISOString().split('T')[0]; // YYYY-MM-DD format
}

async function fetchBinanceKlines(symbol: string, startTime: number, endTime: number): Promise<BinanceKline[]> {
  try {
    const url = `https://api.binance.com/api/v3/klines?symbol=${symbol}&interval=1d&startTime=${startTime}&endTime=${endTime}&limit=1000`;
    
    console.log(`📡 Fetching ${symbol} daily candles from Binance API...`);
    
    const response = await fetch(url);
    
    if (!response.ok) {
      throw new Error(`Binance API error: ${response.status} ${response.statusText}`);
    }
    
    const data = await response.json() as BinanceKline[];
    
    if (!Array.isArray(data)) {
      throw new Error('Invalid response format from Binance API');
    }
    
    console.log(`✅ Successfully fetched ${data.length} daily candles for ${symbol}`);
    return data;
    
  } catch (error) {
    console.error(`❌ Error fetching ${symbol} data:`, error);
    throw error;
  }
}

function processBinanceData(klines: BinanceKline[]): DailyCandle[] {
  return klines.map(kline => {
    const date = new Date(kline[0]).toISOString().split('T')[0];
    return {
      date,
      open: parseFloat(kline[1]),
      high: parseFloat(kline[2]),
      low: parseFloat(kline[3]),
      close: parseFloat(kline[4]),
      volume: parseFloat(kline[5])
    };
  });
}

function calculateStats(ssvCandles: DailyCandle[], ethCandles: DailyCandle[]): MonthlyStats {
  if (ssvCandles.length === 0 || ethCandles.length === 0) {
    throw new Error('No candle data to process');
  }
  
  if (ssvCandles.length !== ethCandles.length) {
    throw new Error('SSV and ETH candle counts do not match');
  }
  
  // Calculate averages using close prices
  const ssvClosePrices = ssvCandles.map(c => c.close);
  const ethClosePrices = ethCandles.map(c => c.close);
  
  const ssvUsdtAverageOriginal = ssvClosePrices.reduce((sum, price) => sum + price, 0) / ssvClosePrices.length;
  const ssvUsdtAverage = Math.max(ssvUsdtAverageOriginal, 10); // Special rule: minimum 10
  const ethUsdtAverage = ethClosePrices.reduce((sum, price) => sum + price, 0) / ethClosePrices.length;
  
  // Find highest/lowest closing prices for SSV and ETH
  const highestSsvPrice = Math.max(...ssvClosePrices);
  const lowestSsvPrice = Math.min(...ssvClosePrices);
  const highestEthPrice = Math.max(...ethClosePrices);
  const lowestEthPrice = Math.min(...ethClosePrices);
  
  const highestSsvCandle = ssvCandles.find(c => c.close === highestSsvPrice);
  const lowestSsvCandle = ssvCandles.find(c => c.close === lowestSsvPrice);
  const highestEthCandle = ethCandles.find(c => c.close === highestEthPrice);
  const lowestEthCandle = ethCandles.find(c => c.close === lowestEthPrice);
  
  const highestSsvPriceDay = highestSsvCandle?.date || 'Unknown';
  const lowestSsvPriceDay = lowestSsvCandle?.date || 'Unknown';
  const highestEthPriceDay = highestEthCandle?.date || 'Unknown';
  const lowestEthPriceDay = lowestEthCandle?.date || 'Unknown';
  
  // Calculate daily ratios
  const dailyRatios = ssvCandles.map((ssvCandle, index) => {
    const ethCandle = ethCandles[index];
    return {
      date: ssvCandle.date,
      ratio: ssvCandle.close / ethCandle.close
    };
  });
  
  // Calculate ratio statistics
  const ratios = dailyRatios.map(d => d.ratio);
  const ssvEthRatio = ssvUsdtAverage / ethUsdtAverage;
  const highestRatio = Math.max(...ratios);
  const lowestRatio = Math.min(...ratios);
  
  const highestRatioDay = dailyRatios.find(d => d.ratio === highestRatio)?.date || 'Unknown';
  const lowestRatioDay = dailyRatios.find(d => d.ratio === lowestRatio)?.date || 'Unknown';
  
  const firstDate = ssvCandles[0].date;
  const lastDate = ssvCandles[ssvCandles.length - 1].date;
  
  return {
    ssvUsdtAverage,
    ssvUsdtAverageOriginal,
    ethUsdtAverage,
    ssvEthRatio,
    highestRatio,
    lowestRatio,
    highestRatioDay,
    lowestRatioDay,
    highestSsvPrice,
    lowestSsvPrice,
    highestSsvPriceDay,
    lowestSsvPriceDay,
    highestEthPrice,
    lowestEthPrice,
    highestEthPriceDay,
    lowestEthPriceDay,
    dateRange: `${firstDate} - ${lastDate}`,
    totalDays: ssvCandles.length,
    ssvCandles,
    ethCandles
  };
}

function displayResults(stats: MonthlyStats): void {
  console.log('\n📊 Monthly SSV/ETH Price Statistics (From USDT Pairs)');
  console.log('====================================================');
  
  const table = [
    ['Item', 'Value'],
    ['─'.repeat(35), '─'.repeat(50)],
    ['SSV/USDT Average (Original)', `$${stats.ssvUsdtAverageOriginal.toFixed(6)}`],
    ['SSV/USDT Average (Applied)', `$${stats.ssvUsdtAverage.toFixed(6)}`],
    ['ETH/USDT Average', `$${stats.ethUsdtAverage.toFixed(2)}`],
    ['SSV/ETH Ratio (Average)', `${stats.ssvEthRatio.toFixed(8)} ETH`],
    ['Highest SSV/ETH Ratio', `${stats.highestRatio.toFixed(8)} ETH (${stats.highestRatioDay})`],
    ['Lowest SSV/ETH Ratio', `${stats.lowestRatio.toFixed(8)} ETH (${stats.lowestRatioDay})`],
    ['Highest SSV Close Price', `$${stats.highestSsvPrice.toFixed(6)} (${stats.highestSsvPriceDay})`],
    ['Lowest SSV Close Price', `$${stats.lowestSsvPrice.toFixed(6)} (${stats.lowestSsvPriceDay})`],
    ['Highest ETH Close Price', `$${stats.highestEthPrice.toFixed(2)} (${stats.highestEthPriceDay})`],
    ['Lowest ETH Close Price', `$${stats.lowestEthPrice.toFixed(2)} (${stats.lowestEthPriceDay})`],
    ['Date Range', stats.dateRange],
    ['Total Days', stats.totalDays.toString()],
    ['SSV_ETH Input', `${stats.ssvEthRatio.toFixed(7)}`]
  ];
  
  // Find the longest strings in each column for padding
  const col1Width = Math.max(...table.map(row => row[0].length));
  const col2Width = Math.max(...table.map(row => row[1].length));
  
  // Display minimum price rule notice if applied
  if (stats.ssvUsdtAverageOriginal < 10) {
    console.log('\n⚠️  Special Rule Applied: SSV/USDT average was below $10, adjusted to $10.00');
  }
  
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
  
  console.log(`🔍 Analyzing SSV/ETH price data for ${CURRENT_MONTH}`);
  
  try {
    const { startDate, endDate } = getMonthDateRange(CURRENT_MONTH);
    
    // Convert dates to milliseconds timestamp for Binance API
    const startTime = startDate.getTime();
    const endTime = endDate.getTime() + (24 * 60 * 60 * 1000) - 1; // End of day
    
    console.log(`📅 Date range: ${formatDate(startDate)} to ${formatDate(endDate)}`);
    
    // Fetch both SSV/USDT and ETH/USDT data from Binance
    const [ssvKlines, ethKlines] = await Promise.all([
      fetchBinanceKlines('SSVUSDT', startTime, endTime),
      fetchBinanceKlines('ETHUSDT', startTime, endTime)
    ]);
    
    if (ssvKlines.length === 0 || ethKlines.length === 0) {
      console.error('❌ No data received from Binance API');
      process.exit(1);
    }
    
    // Process the raw kline data
    const ssvCandles = processBinanceData(ssvKlines);
    const ethCandles = processBinanceData(ethKlines);
    
    // Calculate statistics
    const stats = calculateStats(ssvCandles, ethCandles);
    
    // Display results
    displayResults(stats);
    
  } catch (error) {
    console.error('❌ Error:', error);
    process.exit(1);
  }
}

if (require.main === module) {
  main().catch(error => {
    console.error('❌ Unexpected error:', error);
    process.exit(1);
  });
}