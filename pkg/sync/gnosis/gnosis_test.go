package gnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestGnosisSafe(t *testing.T) {
	var tests = []struct {
		name      string
		addr      string
		threshold int
		version   string
		err       error
	}{
		{"valid", "0x19B3Eb3Af5D93b77a5619b047De0EED7115A19e7", 3, "1.3.0", nil},
		{"invalid", "0x39aa39c021dfbae8fac545936693ac917d5e7564", 0, "", ErrNotFound},
		{"invalid", "0x39aa39c021dfbae8fac545936693ac917d5e7563", 0, "", ErrNotFound},
	}

	client := New("https://safe-transaction-mainnet.safe.global", 0.1)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := common.HexToAddress(tt.addr)
			safe, err := client.Safe(context.Background(), addr)
			t.Logf("addr: %s, err: %v", addr, err)
			if tt.err != nil {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.threshold, safe.Threshold)
			require.Equal(t, tt.version, safe.Version)
		})
	}
}

func TestDeployersTableGnosisSafe(t *testing.T) {
	// Skip this test in CI environments as it makes external API calls
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Parse the deployersTable JSON
	var deployers []struct {
		OwnerAddress    string `json:"owner_address"`
		DeployerAddress string `json:"deployer_address"`
		GnosisSafe      bool   `json:"gnosis_safe"`
		TxHash          string `json:"tx_hash"`
	}
	err := json.Unmarshal([]byte(deployersTable), &deployers)
	require.NoError(t, err)

	// Create a Gnosis Safe client
	client := New("https://safe-transaction-mainnet.safe.global", 0.95)

	// Track differences
	var differences []string

	// Check each owner address
	for idx, deployer := range deployers {
		// Convert owner address to common.Address
		ownerAddr := common.HexToAddress("0x" + deployer.OwnerAddress)

		// Query the Gnosis Safe API
		t.Logf("Checking address: 0x%s (%d/%d)", deployer.OwnerAddress, idx+1, len(deployers))
		safe, err := client.Safe(context.Background(), ownerAddr)

		// Determine if it's a Gnosis Safe
		isGnosisSafe := false
		if err == nil && safe != nil {
			isGnosisSafe = true
		} else if err != nil && err != ErrNotFound {
			t.Logf("Error checking address 0x%s: %v", deployer.OwnerAddress, err)
			continue
		}

		// If there's a mismatch, record it
		if isGnosisSafe != deployer.GnosisSafe {
			differences = append(differences, fmt.Sprintf(
				"Owner: 0x%s, Expected: %v, Actual: %v",
				deployer.OwnerAddress,
				deployer.GnosisSafe,
				isGnosisSafe,
			))
		}
	}

	// Output differences
	if len(differences) > 0 {
		t.Errorf("Found %d differences in gnosis_safe values:\n%s",
			len(differences),
			strings.Join(differences, "\n"))
	}
}

func TestDeployersTableGnosisSafeQuick(t *testing.T) {
	// This test checks a small sample of addresses to quickly verify the gnosis_safe values

	// Parse the deployersTable JSON
	var deployers []struct {
		OwnerAddress    string `json:"owner_address"`
		DeployerAddress string `json:"deployer_address"`
		GnosisSafe      bool   `json:"gnosis_safe"`
		TxHash          string `json:"tx_hash"`
	}
	err := json.Unmarshal([]byte(deployersTable), &deployers)
	require.NoError(t, err)

	// Create a Gnosis Safe client with a lower rate limit to avoid rate limiting
	client := New("https://safe-transaction-mainnet.safe.global", 0.05) // 1 request per 20 seconds

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Sample addresses to check (mix of expected true and false)
	// Choose a smaller sample to avoid rate limiting
	sampleIndices := []int{0, 1, 8, 9, 20, 21}

	// Track differences
	var differences []string

	// Check each sample address
	for _, idx := range sampleIndices {
		if idx >= len(deployers) {
			continue
		}

		deployer := deployers[idx]

		// Convert owner address to common.Address
		ownerAddr := common.HexToAddress("0x" + deployer.OwnerAddress)

		t.Logf("Checking address: 0x%s", deployer.OwnerAddress)

		// Query the Gnosis Safe API
		safe, err := client.Safe(ctx, ownerAddr)

		// Determine if it's a Gnosis Safe
		isGnosisSafe := false
		if err == nil && safe != nil {
			isGnosisSafe = true
		} else if err != nil && err != ErrNotFound {
			t.Logf("Error checking address 0x%s: %v", deployer.OwnerAddress, err)
			continue
		}

		// If there's a mismatch, record it
		if isGnosisSafe != deployer.GnosisSafe {
			differences = append(differences, fmt.Sprintf(
				"Owner: 0x%s, Expected: %v, Actual: %v",
				deployer.OwnerAddress,
				deployer.GnosisSafe,
				isGnosisSafe,
			))
		} else {
			t.Logf("Verified: 0x%s, GnosisSafe: %v", deployer.OwnerAddress, isGnosisSafe)
		}

		// Sleep to avoid rate limiting
		time.Sleep(2 * time.Second)
	}

	// Output differences
	if len(differences) > 0 {
		t.Errorf("Found %d differences in gnosis_safe values:\n%s",
			len(differences),
			strings.Join(differences, "\n"))
	} else {
		t.Logf("All sample entries verified successfully")
	}
}

var deployersTable = `[
  {
    "owner_address": "00b09f79228ef82d5925669ab94d6188df24e085",
    "deployer_address": "344d9c4f488bb5519d390304457d64034618145c",
    "gnosis_safe": true,
    "tx_hash": "e12bea278faad40474c0b5f50fe2bd89d8101f4c35bf2c34fa66869f97c66735"
  },
  {
    "owner_address": "06a45f4a92fa07e93b314790ee03bb754f5da628",
    "deployer_address": "14727c710ba8ef84481b03d718c92812bfcb6058",
    "gnosis_safe": false,
    "tx_hash": "fb58cfc118ce66615525e8742775b805b3b0c87a31579526b8c0bcaf41a01de4"
  },
  {
    "owner_address": "08b804864e367416924775cb96c3d7ba40cc81f6",
    "deployer_address": "0050ee455720cc8baba740a622311f8f2d8ac0aa",
    "gnosis_safe": false,
    "tx_hash": "ca607f78921982dc83035f214a98f64d27c91fc3f5c29aabd7d7f8c3e49d2f46"
  },
  {
    "owner_address": "0921381ffbeac9f5c516762e6e5dd9606682e0b1",
    "deployer_address": "a3cda4cb624a1fc093ea9486bbd47aa9a8774b08",
    "gnosis_safe": false,
    "tx_hash": "f8dabff9c3efa368f1c82b47a9d99112505d55b959da7a7892796db299848ad3"
  },
  {
    "owner_address": "0bacb8024f58add1b22641a791d47aa9eb752acf",
    "deployer_address": "e6b5a31d8bb53d2c769864ac137fe25f4989f1fd",
    "gnosis_safe": true,
    "tx_hash": "04b33c558f5e38de6bf594a8502f3f111aba427fe27ea0c4f5ca3d8572712ef2"
  },
  {
    "owner_address": "16111771895da11e3e1b60693ec4a98fe0c6042a",
    "deployer_address": "83b55df61cd1181f019df8e93d46bafd31806d50",
    "gnosis_safe": true,
    "tx_hash": "0936de7d80a398901ceb72957f430dee62b634e5fa717c5083479aa228a89365"
  },
  {
    "owner_address": "18169ee0ced9aa744f3cd01adc6e2eb2e8fb0087",
    "deployer_address": "5de069482ac1db318082477b7b87d59dfb313f91",
    "gnosis_safe": false,
    "tx_hash": "acf47350fbcb5a7ebad194a6f964aee3e12329f9b66b5a3bfc407cd50bd61aa4"
  },
  {
    "owner_address": "19c4016b667f8f049a6d0b93855141dab341f44d",
    "deployer_address": "86ac462eb1524efd9652e5833f844232da3ddde5",
    "gnosis_safe": false,
    "tx_hash": "12db6a2d084f075d5795a371e4a3a7101461bda3aa03ca5bf35a2ecd5ab34836"
  },
  {
    "owner_address": "1c4eda5e2be9055126b9833db4ea99a30822f751",
    "deployer_address": "64867f5bb09aea861c6eba9886a45983e76f0e99",
    "gnosis_safe": true,
    "tx_hash": "989f3f84af8e4ce218bf25e61e870202d322737e2bea4088e9f37c7f646ad765"
  },
  {
    "owner_address": "1d9903f7b14f6e12a278bb2e83f51bd9429ab482",
    "deployer_address": "98720c4710148c8cb99684fbda9e04f4274fc875",
    "gnosis_safe": true,
    "tx_hash": "cf4886c689684e3d4bb61f5bea9825eee5a2ee817ead228e605ec8615a560fdb"
  },
  {
    "owner_address": "1ddb4e46810806b5fbf67ac69b84ca48b8cbed1f",
    "deployer_address": "dd3964af0e325f9a56e6d556539050ac0f908952",
    "gnosis_safe": true,
    "tx_hash": "b004ccc07cfaba1ad6c92ec55570552ca7bb5e8ae9c4a003378d61f6c37eeafe"
  },
  {
    "owner_address": "20313af216272eff3285cdc0be862fa9ae3a0ca3",
    "deployer_address": "f02ea45d3f350f5bca63fa13908ac39ae2cc2180",
    "gnosis_safe": false,
    "tx_hash": "c80cf0a82f1aac20daebefc05170d60e5d4bf29f10983c6cd286ea6c0a8e417e"
  },
  {
    "owner_address": "21979d8e139cf5344f9a6858196126b9b6d96d88",
    "deployer_address": "5b3ef7ed14ab4a240b8290d86a5b1e662e1d618c",
    "gnosis_safe": false,
    "tx_hash": "fbda63966ae7f2b25a3080cfb46db7e173663db7de5950f19021208752f0febc"
  },
  {
    "owner_address": "25fc506dcf9833d897660371c05ceea3970cfa2d",
    "deployer_address": "a04d1171435cffc79cfd07aff988417ed56b3653",
    "gnosis_safe": true,
    "tx_hash": "d3739b6010df1b01e1fc8ab4f8dcf0a7bf2070042584f4c88f261df52c262c82"
  },
  {
    "owner_address": "26c212f06675a0149909030d15dc46daee9a1f8a",
    "deployer_address": "8a25d8c9fa8c7a726137f2d618d85cbc2c083f78",
    "gnosis_safe": true,
    "tx_hash": "11fab1a5594f49762594fcae35db9eeb233b457895670ce7e3471db26949c732"
  },
  {
    "owner_address": "29984aadadb3927fb8c0cf5a539a282f39066332",
    "deployer_address": "62a90760c7ce5cbadbb64188ad075e9a52518d41",
    "gnosis_safe": false,
    "tx_hash": "7f1ed3402f3283c876a1da81e47ef944debc4e5c9cec6df7859b09ab8db912d8"
  },
  {
    "owner_address": "29cada9320a4d068d1f4651b9ac0aa10745317ff",
    "deployer_address": "4ff2fa3a8ea8a12dd54e2ca0eaf02da785c660ef",
    "gnosis_safe": false,
    "tx_hash": "899c7c80601810fab919e3c981b0b145467794d4eb1fde0c2ef56bf28b1d2bf0"
  },
  {
    "owner_address": "2b17924d0d2f3d70aefb07602c7926827677ba19",
    "deployer_address": "6e243e2477edf755fd796aaeaac797ae7828f759",
    "gnosis_safe": true,
    "tx_hash": "aaa05e1b9e17d4e02d87c1baa57ee1560bf6f6982a6f02d4411b834dfe036265"
  },
  {
    "owner_address": "2cb72bc8176e6056f5090bdf5f6497acab327a5f",
    "deployer_address": "1115f1d81a4027f5655b0955deb55c8a1c7ba6bf",
    "gnosis_safe": true,
    "tx_hash": "a5731e146f5f0449472cbb9b7a96e8fda355ab04b8d5f0e6857f743a3f4d5acd"
  },
  {
    "owner_address": "2ec75fd873860151eeeec4da50e9594bd168ef98",
    "deployer_address": "0f5842f9894c5f4205b87da93b48c21ea9fbe30d",
    "gnosis_safe": true,
    "tx_hash": "3907bc9920b98621e029f94fb4fdbf5dda48e949dc81ef41ee5e54c2c77dd74b"
  },
  {
    "owner_address": "306b5c7475b97bf6df43dfed00a268e5ddcae75e",
    "deployer_address": "f76f2479d4a3d96134c0e64008df612662864520",
    "gnosis_safe": true,
    "tx_hash": "e7eaed7aa453ab6b02744f8797e9d0fa2c264a11904dc8de6b55cea53e8a61db"
  },
  {
    "owner_address": "34edb2ee25751ee67f68a45813b22811687c0238",
    "deployer_address": "4b91827516f79d6f6a1f292ed99671663b09169a",
    "gnosis_safe": false,
    "tx_hash": "eb2ba5b203fb2de462a9171f906556269e09e0ea6c6fae49c3dd000edc316b2b"
  },
  {
    "owner_address": "36c930feceb25dc4418438d171c4a4dbf6712896",
    "deployer_address": "d5ac23b1ade91a054c4974264c9dbddd0e52bb05",
    "gnosis_safe": true,
    "tx_hash": "09d52eac962d57821a773c8be5e7d46f391daca9b88d4ba113c65c79cdcfd8dc"
  },
  {
    "owner_address": "37ed9424465f9ff2f6a012e688de8735cb9e2afb",
    "deployer_address": "9f6b7f6a018955ae8a98c4f0e6d45f2b3c1d70c4",
    "gnosis_safe": true,
    "tx_hash": "d53cc8598f3c0838756622c76c68868d112594413afef0b36165e68619a77b19"
  },
  {
    "owner_address": "3b3bca3d0296f3e3a2a10197bdb8515ddf59f2eb",
    "deployer_address": "bd3874a535c8c7608ba2bc4384c4c03d112b0d96",
    "gnosis_safe": true,
    "tx_hash": "85737576397a883f510b21f35e9168007100a228649b309c020bef82e851fe6e"
  },
  {
    "owner_address": "3fb35698c5543e4c87c19751ad3646ab339b8319",
    "deployer_address": "6a54cf0befd629a8f74348bb622a84a63f944532",
    "gnosis_safe": true,
    "tx_hash": "feb2a7421d1439e704e42608705dcf5dbbc81119243bef2abbd527a030305af6"
  },
  {
    "owner_address": "411fa6e02e08d0dd0db3b9167f8c349039288954",
    "deployer_address": "a53a6fe2d8ad977ad926c485343ba39f32d3a3f6",
    "gnosis_safe": false,
    "tx_hash": "24f60c42b01d7294d51a21491192d6a15daba95e27a7ec60b11cdf50371dfed4"
  },
  {
    "owner_address": "45617dd06e4224d5f3acaff4ef3a067c68119a89",
    "deployer_address": "dcaf3ed6e28047f4480900a39a318d8377ad36e3",
    "gnosis_safe": true,
    "tx_hash": "3bfd4b99f0c5f3585590e341490cc384f13e3e573ce15f751f87711257bda98f"
  },
  {
    "owner_address": "45acf8f7a8232ee4cee6294de58075c1565d4df3",
    "deployer_address": "9d4fd64feb016eab2ee450703f4efc1b2eb14deb",
    "gnosis_safe": false,
    "tx_hash": "916219b7015088f956ffd88e4605b3ac431f93073cb5e056ff3b2a7b73aa1d17"
  },
  {
    "owner_address": "4685db8bf2df743c861d71e6cfb5347222992076",
    "deployer_address": "4b91827516f79d6f6a1f292ed99671663b09169a",
    "gnosis_safe": false,
    "tx_hash": "bdbeb5587c69f1bbc125539e591f123a163c44d95206a8e4a191b5d3efc0a89e"
  },
  {
    "owner_address": "4c46fecc566a196cd3196d19899d9a9d73df5b56",
    "deployer_address": "95ae2751d50f992561e0351860077d19879a5cbc",
    "gnosis_safe": true,
    "tx_hash": "e0a2bce2d541315f39a5de75e2471113730e3d590dbf6b4af0631f673267921a"
  },
  {
    "owner_address": "4dc0bf9b18f9c550786a67ef42569d6337c4e78c",
    "deployer_address": "d4f962494c3f70244bf3dd3a2c55132da56da880",
    "gnosis_safe": false,
    "tx_hash": "c444beac39f851367d608996bec8a9a01c7c765fdef88ff93ec63f257916c1b7"
  },
  {
    "owner_address": "4f6e412580e7a93a104836d596f8d6c8be0ef431",
    "deployer_address": "ca30d150b590826a4633a5f99e05ad6571f9bb66",
    "gnosis_safe": false,
    "tx_hash": "6bb3e9dafeb0c599ed136b3d065811ee871fcc4f473d8bfec9aadbae1c7a0e8e"
  },
  {
    "owner_address": "5071e29f49f9b008267d2ed76d54b32d91695cde",
    "deployer_address": "3202b5b0602274197ccb22b8c02cec9539990c7c",
    "gnosis_safe": false,
    "tx_hash": "851a3ae8e2eac84fced6f012b5faea260db18cdcf61d38c9f8fa65146882c6e3"
  },
  {
    "owner_address": "525c9c957f6b5796d0521b5c04313a8466e2a4c1",
    "deployer_address": "776e273eef19cf80c1ea17b193fc86c3b581995a",
    "gnosis_safe": false,
    "tx_hash": "1f48d8a467b837cd6586478b56ef7794d11a51c146cdb8f32d53397e99c0ebdb"
  },
  {
    "owner_address": "537a684be8f528172a68c987c15ff45a4c82ebb2",
    "deployer_address": "c316e05be8c3b688c0276be3149010391e8a58e6",
    "gnosis_safe": false,
    "tx_hash": "a12ad922b837b9442979718d38a17aa7d8f03439035b04cd550bdf8518b855b7"
  },
  {
    "owner_address": "57cea99437a5e01eabb137134f1ced8bdba974f8",
    "deployer_address": "3e1fb84db94e326887ab9c6bedaf5060924c39f8",
    "gnosis_safe": true,
    "tx_hash": "46e4cdfd384652af518c1d33623a780b8ceff48c39006c3aebd8b373acb8d3f5"
  },
  {
    "owner_address": "5b2b6a36ed514eb02aa8c61e18ea75a5b4520159",
    "deployer_address": "a3594a4bd05bbefb213ba88e3b969207365d9e81",
    "gnosis_safe": false,
    "tx_hash": "2c052548db4e88388b4998a391489197b2dc821030c88823a40dad158e6ac56a"
  },
  {
    "owner_address": "5bbe36152d3cd3eb7183a82470b39b29eedf068b",
    "deployer_address": "d2fd442a68cc17a967e31b4712df110a6d0ff513",
    "gnosis_safe": false,
    "tx_hash": "302f392c21555f90a87ed22f7cdf6359f55dbe85267c394201684b10b5b38eb5"
  },
  {
    "owner_address": "5ed21077adef79628516b7628839c0cd2b44ca5f",
    "deployer_address": "fef3c7aa6956d03dbad8959c59155c4a465dcacd",
    "gnosis_safe": true,
    "tx_hash": "d6ce0cc0c34a90b0fc333f6543eee3730738bddfa59a1a13c9fa2ffaeb96ea8b"
  },
  {
    "owner_address": "5ed8b5b1bcac0adb3205a779a70b7e6cc285c2bb",
    "deployer_address": "62a90760c7ce5cbadbb64188ad075e9a52518d41",
    "gnosis_safe": false,
    "tx_hash": "8dd24fe5be997cb17e706254b102a45f4e7c285f7f2dc3f66c77145949c6bfa4"
  },
  {
    "owner_address": "5fae2734cd11029d8bccbcdc3752bf0299c0934c",
    "deployer_address": "bd3874a535c8c7608ba2bc4384c4c03d112b0d96",
    "gnosis_safe": true,
    "tx_hash": "50131809be4d74cc293dae19eadc4747c367cbf43cc3df90ce10cdb0cc24a7a5"
  },
  {
    "owner_address": "6161a36b7bd4d469a11803535816aac9829ad5cc",
    "deployer_address": "f304a4229561aeba13425710acf1f46c9f24f1eb",
    "gnosis_safe": false,
    "tx_hash": "d79d443f6239ec413867faaf9cd2cad1ed262ab95031b621ac94398dfff441ea"
  },
  {
    "owner_address": "6706a45d13685950308eff93b45d1a4ebb054f84",
    "deployer_address": "0c5b4415bf1b415ace886e2fc0a9feebc05e6e91",
    "gnosis_safe": true,
    "tx_hash": "7a854d9ff2fed46e00fa6d507ff4235920926707952e995b42beab4d2167c08c"
  },
  {
    "owner_address": "6708541e1fe8dff3a765b4dd402a7abbcd9b0622",
    "deployer_address": "e2e8a6f12b928ebae8d2bb3e80c25885e4123d00",
    "gnosis_safe": true,
    "tx_hash": "6106b1f2d74c28ed31902c0217cced4a712e0ea83575328e805b5f94d414640d"
  },
  {
    "owner_address": "67cd91fa3f9bbd96c7f5fc59a54b1b85a4c3a50b",
    "deployer_address": "aa41ca850323660e85af507548449f3aba2b5a19",
    "gnosis_safe": false,
    "tx_hash": "bb48595eb3c8dd6a0fd149a0335edd6cdb550335c0ac2c6517a9ea1da88facaa"
  },
  {
    "owner_address": "6929ec47b85731684b81daed082b23aaaba659f6",
    "deployer_address": "67460a8ee0596a251e8f002b60fea82e48f1a122",
    "gnosis_safe": true,
    "tx_hash": "e7d21881e775ff2b484111b5d48bd8d6321081de75856bf171afdf9070ebbf47"
  },
  {
    "owner_address": "6b5de3b71f927fb3988f3ae8254ccc2bf6b20b14",
    "deployer_address": "3af2189c656890d55701ed290b532885844eea5f",
    "gnosis_safe": false,
    "tx_hash": "98ef55336a86bf63ca70bd92b26f91031f326aca4490e3f2571c4d3ce396304e"
  },
  {
    "owner_address": "6e809aadf0e4686322d53c611e9facf9cfb0f636",
    "deployer_address": "6c7c332a090c8d2085857cf3220ea01c6d45a723",
    "gnosis_safe": true,
    "tx_hash": "58fae5960abf3c420969063959bdf0f934497c707f75bec1e56b90fe59563bee"
  },
  {
    "owner_address": "737483bc858d6bb66962e27a4bd8612cca818d37",
    "deployer_address": "e86a0f19e06146d1c30bb37607d549077b3541b2",
    "gnosis_safe": false,
    "tx_hash": "e890cc15373c76417125a38077854d0ad40a15b1f95896fad151a9242c3ae35d"
  },
  {
    "owner_address": "7465169a70212f7069623e7ea4e86605a8096d54",
    "deployer_address": "f37fef00fe67956e9870114815c42f0cc18373ce",
    "gnosis_safe": true,
    "tx_hash": "8bb748d26161ce90f704214dbc5edf4c13d16cb35d77d30ac94e22ce570d0404"
  },
  {
    "owner_address": "759c1d68eaeb808bb56519d59a1f49244086a892",
    "deployer_address": "864476c5db200e3d13e70b72d75f1d3d0f5947e3",
    "gnosis_safe": true,
    "tx_hash": "c8d55c5979c2ed95801243782ff4566fdb7c8db2c1e52f9fd4fee3b777a56fc7"
  },
  {
    "owner_address": "75f9af7483ef01635a6b80ec1b1f51c2024e22c8",
    "deployer_address": "e377bf804fe3d823e5dc7b93b15168b8a9566249",
    "gnosis_safe": true,
    "tx_hash": "e0735cb6601d0d7bc45514a0bd24fba333ae0b3d5d5cdb75f5518e7733c85719"
  },
  {
    "owner_address": "772b4fb7c9c221be324df548b295ccfaecd9941b",
    "deployer_address": "aa41ca850323660e85af507548449f3aba2b5a19",
    "gnosis_safe": false,
    "tx_hash": "ffd71e56bbb626ccc7cfd3cc8d9f945e586c45b9f24ee624191cc3b18fad1a4b"
  },
  {
    "owner_address": "79299b19f1aaad319815dc01294c99d91f9d36a6",
    "deployer_address": "f37fef00fe67956e9870114815c42f0cc18373ce",
    "gnosis_safe": true,
    "tx_hash": "073bd868278f78b0d645ecee1faad75a880c7093c016f2842625f391ed827f66"
  },
  {
    "owner_address": "7954dd93133365fd59ec782a035023fc3ae76b2c",
    "deployer_address": "d85e2f52af375bf451c7505838af36a4ce4b99fe",
    "gnosis_safe": true,
    "tx_hash": "6705dfcda9ab05beca279129e59f481e18d182b2043cb62cf8d998f3869f01eb"
  },
  {
    "owner_address": "7a13d0860260346890a713bcdfdb0b7012e8de2a",
    "deployer_address": "d624feff4b4e77486b544c93a30794ca4b3f10a2",
    "gnosis_safe": true,
    "tx_hash": "a965415bc6c69c869c9f77c8b496c31249237075e621a35bb17f3af5ffb4c1fe"
  },
  {
    "owner_address": "7bf6945430017aefea7dd0cd450382aae01220f5",
    "deployer_address": "7a82e643b0c8ae619f71d9667559180c33c92277",
    "gnosis_safe": true,
    "tx_hash": "f4facc8576a4d7aa9adfc2956e0a7bbe0d3b77daf908dcc4e0264f2df8189a66"
  },
  {
    "owner_address": "7dc028d840923ecfa8a1c088700e474e3214b52f",
    "deployer_address": "f4cbb755be3c9eec013f67dbc1896efccbefcd2a",
    "gnosis_safe": false,
    "tx_hash": "038753362fb303eda41cc98fee9e66dd384fde877a6f6dd8f66d1071e83c6311"
  },
  {
    "owner_address": "840541529e28564c1795dfefe5fec26fa557f778",
    "deployer_address": "f37fef00fe67956e9870114815c42f0cc18373ce",
    "gnosis_safe": true,
    "tx_hash": "1bf0fbe122b5ce426875ce0f88c78b614520fa443115514d37d012a6c6de6447"
  },
  {
    "owner_address": "8682982ad244efc789dc60eeb6a4823fd883319a",
    "deployer_address": "dd3964af0e325f9a56e6d556539050ac0f908952",
    "gnosis_safe": true,
    "tx_hash": "7b6f85759a5d5b35bd3cd0489330e6fef9e57e0dfd72f18c49ab6a0e10318fea"
  },
  {
    "owner_address": "87393be8ac323f2e63520a6184e5a8a9cc9fc051",
    "deployer_address": "36e655069464be6202e0e4d5ee9f76034c0ad9b6",
    "gnosis_safe": false,
    "tx_hash": "d08ac985c6678a2dd3c29431bf555df004a84cc02655e17ba16611f9bb461efe"
  },
  {
    "owner_address": "885a91006cc369439251b1a7a8cf7b8d2e1432d9",
    "deployer_address": "08432468fdfe4359519995ddea5c097e0dc6d338",
    "gnosis_safe": true,
    "tx_hash": "8270ad732523d0dc48da777f3523d72a870253c2b04b53f06482601e4a653bfc"
  },
  {
    "owner_address": "88f9518919f051f0845a34b793897a941d84d43a",
    "deployer_address": "ae38e358f871aae431a74c82e53fe81e0a13deb9",
    "gnosis_safe": false,
    "tx_hash": "744bc2beef83d6ef9dde693105625a0ed6b1bc8e27d4a24b7511579feb923751"
  },
  {
    "owner_address": "8b87d5d4ccabc26a99d44bec19c5f25f0a0d6019",
    "deployer_address": "8ef3fa40a403d4174da03769f3b514e729ff0cd5",
    "gnosis_safe": true,
    "tx_hash": "7fe6adfda14a22c81136c67cf6ae9bbd0f8721e37e2bfcb725c6342d466e6b1d"
  },
  {
    "owner_address": "8cbe11227b437c842fc4c93402d5088dc8044137",
    "deployer_address": "12227dfe5363cbe55919e230653810de0ff317e2",
    "gnosis_safe": false,
    "tx_hash": "d3f75b21664465add10edfafc3a920268026181b9db4003ebef1d381b0c3812a"
  },
  {
    "owner_address": "944991724cfa9e218f73ba03608913da9a21f9b7",
    "deployer_address": "971561f9ab29acbd6d1dc7b17f0bb6c386ad311b",
    "gnosis_safe": false,
    "tx_hash": "7f74b45be64d92a38bcdde78b500adc3dbc499c68b1e6cc655a0212f17642b4f"
  },
  {
    "owner_address": "9501a0da5ac0671b6744d1c951863ac2b794d282",
    "deployer_address": "71955e1b30e2b0585188466f1b241b6004d75dc4",
    "gnosis_safe": false,
    "tx_hash": "c58159d71624b120a1bb62e516a5421e7f9ad9b7d66508ce89fff2860aff8d4c"
  },
  {
    "owner_address": "96c9ad3a5df8496f9a7196e79e88682dc556fb87",
    "deployer_address": "d4792f3cb4379b4832a1de78bfb642b06cbc195c",
    "gnosis_safe": true,
    "tx_hash": "ac118b060341fa70e0ca32caef503148d163b012c7377f6f71dc78148d6756ae"
  },
  {
    "owner_address": "96ff7250712442ca5e11fd02c52fa092357b71eb",
    "deployer_address": "3d96d2d69433a3b3bf7921177b15ce9849e8fa9e",
    "gnosis_safe": true,
    "tx_hash": "153090412e765f05a5be9c6f0af59ebe31e0d0ab399e1a40a83a5e5a02fda7fc"
  },
  {
    "owner_address": "97e18544e156724e4076945f10c288ecbbc94e54",
    "deployer_address": "08432468fdfe4359519995ddea5c097e0dc6d338",
    "gnosis_safe": true,
    "tx_hash": "5875bfb5ba32132f1bdf01d7ffbde736601358c9f4ae2b4ca46d90f73ba07bc0"
  },
  {
    "owner_address": "9e7dba478ff82243d07bc88a74319c0e35c802b1",
    "deployer_address": "f95aa110636f466ddec95598e6c661b921243665",
    "gnosis_safe": false,
    "tx_hash": "415e15e3170573d6b7df9e28bd207fb9ebf2598952394294d77c8d810e3c6535"
  },
  {
    "owner_address": "9ea5c73eef6fe5b1df238930c5a3b8dd82ff1422",
    "deployer_address": "8ef3fa40a403d4174da03769f3b514e729ff0cd5",
    "gnosis_safe": true,
    "tx_hash": "f11c1188a468f55b96599614c5fc3b4380bf69b497b2d9c45d2163fa3d4bfaeb"
  },
  {
    "owner_address": "9ff4beea2ea61f37b0da40da12e6daa88b419c02",
    "deployer_address": "f37fef00fe67956e9870114815c42f0cc18373ce",
    "gnosis_safe": true,
    "tx_hash": "e51c5d7bfa43a526b011cf1d9a29a8c06c5817391989eb847fdb86d9515ae61b"
  },
  {
    "owner_address": "a456c36582ad81ca6091dd78e0c97ba309ff11e9",
    "deployer_address": "02b97fec5023f321865155304c71aa0c3db25d29",
    "gnosis_safe": false,
    "tx_hash": "d2a8181c93a84c0038af1161494909e9d7c53c805b06aea5b0b5df63ca919ee9"
  },
  {
    "owner_address": "a5f4ed518286ed614fe317c95d8a287a94c923c9",
    "deployer_address": "726849ba03d72b5c4f58da88e4709df4a461ca01",
    "gnosis_safe": false,
    "tx_hash": "7be2f48dbdeeb5b22b89e8630d14e68ef82056aae9c1a6bc6a3f077afe3102e7"
  },
  {
    "owner_address": "ab80312f79209409b638b261c61fe73070d12818",
    "deployer_address": "cfafc6bdd1b92e510d5409ee460bb1a712165aa8",
    "gnosis_safe": false,
    "tx_hash": "2b312cec45acda7432d752db734f0e768cc096e1a971827d53f34b6a41f8a155"
  },
  {
    "owner_address": "af5f9d8e61b37777d67e9647c1815e79c599d4c4",
    "deployer_address": "62a90760c7ce5cbadbb64188ad075e9a52518d41",
    "gnosis_safe": false,
    "tx_hash": "0641a80bf359dcf3a892c77c7b38f823836cb24bb306965cd3309c071e637d18"
  },
  {
    "owner_address": "b27a34dd7abcb8133503c989d0c95be1cd1ad34a",
    "deployer_address": "2cf21faa2d4e3f5c6517def04e1b3912da22d5ec",
    "gnosis_safe": false,
    "tx_hash": "7a47eeedfaace292a88cf5cdcc0e2b3afaa04be67395da952ae88287db6fae1c"
  },
  {
    "owner_address": "b5f1c25b24be33dc3b67274f0ad0b81c3e38606f",
    "deployer_address": "a3cda4cb624a1fc093ea9486bbd47aa9a8774b08",
    "gnosis_safe": false,
    "tx_hash": "80cde94c5f234e8ee6891cafb39fed135521115fbdbd0e0a04fd5154e7fe4b1b"
  },
  {
    "owner_address": "b8e36e5f926ee4928ce564050148c902f7cb782b",
    "deployer_address": "a4bc74b650241ae2f90225b5789d687c2e26b440",
    "gnosis_safe": false,
    "tx_hash": "a7f539af985086cdda1ec278006c002ac3ae7fecaa4c8bcccae9e7a54d8f66f0"
  },
  {
    "owner_address": "c2d42368d94e2d5d82f3b05a06ec53ebfb81ce0f",
    "deployer_address": "f304a4229561aeba13425710acf1f46c9f24f1eb",
    "gnosis_safe": false,
    "tx_hash": "c1282c440315b5e814c03204eade2bf7f301b821c1f3fc9fa5b58f039d3bae8a"
  },
  {
    "owner_address": "c2d594167628b6e23083af50268e897cdeb61a31",
    "deployer_address": "f5868a259e835957725bd148ff1db42a0b7b5999",
    "gnosis_safe": true,
    "tx_hash": "2b43ce045f2550d5433b58fb4bdef568c0f8c3d1f5c17bdfef0d84245d6a8157"
  },
  {
    "owner_address": "c4c04add0fe21ecc4f186893bfc714da0cfd5ac3",
    "deployer_address": "7754da93ede90930025059d7604296dcf8cea241",
    "gnosis_safe": true,
    "tx_hash": "ac5480d6dd55c31835da5dbc6e6c7496a5a174a1204cd17845e474e74ed279ff"
  },
  {
    "owner_address": "c8d12601f8680ef2408dd4fd17c008817dbc36e0",
    "deployer_address": "a2d25dc3cc4c06662ec4a8d22b5a37342a6bd0eb",
    "gnosis_safe": true,
    "tx_hash": "3dfbef9945ea5e2e1da140db2aa8026861fcb253ee1d2440150f0708e1b78a8f"
  },
  {
    "owner_address": "ca787cdb322df3ad5b9e230baba8346f1bf8fe8b",
    "deployer_address": "9f244da4b90f5798a2d9df193c873048cb670ab6",
    "gnosis_safe": true,
    "tx_hash": "1d52f490bc93d015ff25b5280f22c6eaff7d51edcfef61bc4db6bca2cddaa902"
  },
  {
    "owner_address": "cbcdd778aa25476f203814214dd3e9b9c46829a1",
    "deployer_address": "7aad74b7f0d60d5867b59dbd377a71783425af47",
    "gnosis_safe": true,
    "tx_hash": "e393a66db1bdf2dc0c97b4ab95c460521d3ea518561666f21abf4e684379a8b4"
  },
  {
    "owner_address": "cef46057e46a9b73f77ca048f7fd511234525c39",
    "deployer_address": "08ff7150cdeb62a81418ebe0f1125faea3044ade",
    "gnosis_safe": false,
    "tx_hash": "33db156585442f6de076257b2ba03f82e8f9bb5cf78e7c91ea3910a3efd4ac93"
  },
  {
    "owner_address": "d1208cc82765aa4dc696117d26f37388b6dcb6d5",
    "deployer_address": "46cba1e9b1e5db32da28428f2fb85587bcb785e7",
    "gnosis_safe": true,
    "tx_hash": "e7eeb7f143a0e891210618caa454f5909a00da9e7b7b6c8a81d3796c6c803952"
  },
  {
    "owner_address": "d8af7e871b20c6a84d78ef458bbc252d39802104",
    "deployer_address": "0b63d2376418700db73abce955b3cf03a352a310",
    "gnosis_safe": false,
    "tx_hash": "2a0e452db2fde67751a20b2a9e930c8ebdd47619d5e3c166ade081dd2200506b"
  },
  {
    "owner_address": "da1942e8e5881dfc850bdeca0c4777d92c322184",
    "deployer_address": "f37fef00fe67956e9870114815c42f0cc18373ce",
    "gnosis_safe": true,
    "tx_hash": "b712a03182b5fdf4491828da5068b8f19dd9c9ad6d52e1733d471831e01095fd"
  },
  {
    "owner_address": "df804557b8ef13c4b3b8bb3e36375893492ca0a7",
    "deployer_address": "f7ccb31d05bfb847e59bb9607ea74b816ebfdcc2",
    "gnosis_safe": true,
    "tx_hash": "3c79127f0f8c3aad05938b0a5f40bcb862ba93d3ab8b776b2a6606017013028b"
  },
  {
    "owner_address": "df8f9a7f1f8eb645dc3c95354d2c909c2fdaf0e3",
    "deployer_address": "0cdb34e6a4d635142bb92fe403d38f636bbb77b8",
    "gnosis_safe": false,
    "tx_hash": "ad0f4e1c7ef2f3526c8e2442e34ed1750e028047781cd6f73f0b3dff4c13d153"
  },
  {
    "owner_address": "dfc1107db389f541f0eb2c0d1422544787b82bd2",
    "deployer_address": "ac2bdb872655572f616879eb32e5fb81f336974c",
    "gnosis_safe": true,
    "tx_hash": "4ada92ef3a8ea535b512b78c17c82607a20dcc22d2dfd8b67f279dd11df07a5f"
  },
  {
    "owner_address": "e20b1678ae31e02a1b16693852328c77a4913b72",
    "deployer_address": "0cdb34e6a4d635142bb92fe403d38f636bbb77b8",
    "gnosis_safe": false,
    "tx_hash": "9c5bb0d0805759975704a126fa8fc40b5b91c314b9a57c64105b9c3df47fa25f"
  },
  {
    "owner_address": "e22de1d7a27afab02fc25f9b2b0cb538a26548c2",
    "deployer_address": "20b1b60d595d9bc4a1e5afa24b8a90da409c1c6f",
    "gnosis_safe": true,
    "tx_hash": "28977a9ea3b603ee8d8e367571c2e9c3c4c4da93b59d7659732704c0269c63da"
  },
  {
    "owner_address": "e468a9e3d1fc6983d6aea9266d99a5daeebac58a",
    "deployer_address": "e2063aa95b35f8121a5e2f58bfe6a985270aba77",
    "gnosis_safe": true,
    "tx_hash": "25fa8c052991a9a834c40acb4546f621003f9cda8e35bcf4c50bc0eb501503b4"
  },
  {
    "owner_address": "e58447a964cf024d4937f53c884e4e6fc4a0514f",
    "deployer_address": "342433010665645177648141e12efc3138625cb0",
    "gnosis_safe": false,
    "tx_hash": "20e53573d32d3fc5e420e25d14b7493733804137fb5262184d7ad7aef82a0f86"
  },
  {
    "owner_address": "e64d3d794e309d91b2b7ef4ae941cbe713a72d3a",
    "deployer_address": "ee77a176baea6c8239fef04a6dea02027933f416",
    "gnosis_safe": false,
    "tx_hash": "50441fc48db22a45faa69b53a39f08416ff4d239934a20acf4cf214450b0b2d2"
  },
  {
    "owner_address": "e98538a0e8c2871c2482e1be8cc6bd9f8e8ffd63",
    "deployer_address": "4b91827516f79d6f6a1f292ed99671663b09169a",
    "gnosis_safe": false,
    "tx_hash": "a8492e63e9c7353e8d4d54868ecf1e18f130cd7883b5db14c5b5fe68a3e3ce80"
  },
  {
    "owner_address": "eeb77a24dc66658223cddba668b72e812f3fde67",
    "deployer_address": "aa41ca850323660e85af507548449f3aba2b5a19",
    "gnosis_safe": false,
    "tx_hash": "84b71d1a0de2129a2d96992d6850baa4f6b050e9e2b3c4b489cdd6c220d7f1f1"
  },
  {
    "owner_address": "f05aca9b9d401f2d2b6dc674c037949b011d11dc",
    "deployer_address": "f37fef00fe67956e9870114815c42f0cc18373ce",
    "gnosis_safe": true,
    "tx_hash": "dc5a71c02b5c09e63bb52a533685088af0fc8a15fa9156438eb2f716a13ee76b"
  },
  {
    "owner_address": "f39ac5187ef76203fe800f0beda87a148561b341",
    "deployer_address": "5c6c197c27d5bf73929e1aba7d451bbdf53e6ce8",
    "gnosis_safe": false,
    "tx_hash": "77b4f62c406dd0621d6ad276fe3c92ae9795c7ad57b32c529fa3fa4e5f39169a"
  },
  {
    "owner_address": "f97365034279d638d0094edc638075096bcbf373",
    "deployer_address": "0d6301a8ad83b85b74b91952893a6f7e5f70159e",
    "gnosis_safe": true,
    "tx_hash": "cf129ce0d89048fc791a4f0ceb9be543d81a0506ccc50f5f6cc7f1e386ad8dcf"
  },
  {
    "owner_address": "fc23dd69b6968c8623db910984886c94cc2236aa",
    "deployer_address": "4da68974e9ff1fe5c3d9fb6f4954114fd4399c5c",
    "gnosis_safe": true,
    "tx_hash": "62ce4fc3314042d42c96d553177eacfcd7b3bac06c80e12ad4c4e3ef8f7aa131"
  },
  {
    "owner_address": "fea4e5869b38815533044fa08baafcb87354e66f",
    "deployer_address": "46fe7ee7c4e15406ae79e09b7087a61ba1725ba2",
    "gnosis_safe": false,
    "tx_hash": "11316022aa99b2726aa8e8f915023e60751893a08fe2cae0ac19ce6554b60489"
  },
  {
    "owner_address": "ffdd2fa0d14ccae81508c6ae678ee0cf6e468332",
    "deployer_address": "dfa41eb101670e94779af80816ec682f9999795e",
    "gnosis_safe": true,
    "tx_hash": "9f97b26604970b15c7083dbd4be57e6172da48c446ceea43d24f688d0a875cc6"
  }
]`
