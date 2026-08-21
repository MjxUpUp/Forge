package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/MjxUpUp/Forge/internal/nodeid"
)

// bundle_sig.go — bundle signature sidecars (docs/design/node-identity.md §3/§4).
// The signer signs the whole-bundle sha256 hex digest; the sidecar sits next to the
// bundle as <bundle>.sig and travels with it (project sync pushes it into the node
// prefix). Verification policy lives at the IMPORT side (trust store verdict
// matrix); signing at export is unconditional and cheap.
//
// bundle_sig.go —— bundle 签名 sidecar（docs/design/node-identity.md §3/§4）。
// 签名者对 bundle 整文件 sha256 hex 签名；sidecar 以 <bundle>.sig 伴随 bundle
// 存放并随之旅行（project sync 把它一并推进节点前缀）。验签策略在导入侧
// （trust store 判定矩阵）；导出侧签名无条件且廉价。

// writeBundleSig signs bundlePath with this node's key and writes bundlePath+".sig".
//
// writeBundleSig 用本机节点密钥签名 bundlePath 并写出 bundlePath+".sig"。
func writeBundleSig(bundlePath string) (string, error) {
	f, err := os.Open(bundlePath)
	if err != nil {
		return ``, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ``, err
	}
	id, err := nodeid.LoadOrCreate()
	if err != nil {
		return ``, err
	}
	digest := fmt.Sprintf(`%x`, h.Sum(nil))
	sig, err := id.Sign([]byte(digest))
	if err != nil {
		return ``, err
	}
	raw, err := json.Marshal(nodeid.BundleSig{NodeID: id.NodeID, PublicKey: id.PublicKey, Sig: sig})
	if err != nil {
		return ``, err
	}
	sigPath := bundlePath + `.sig`
	if err := os.WriteFile(sigPath, raw, 0644); err != nil {
		return ``, err
	}
	return sigPath, nil
}

// readBundleSig loads the sidecar if present (nil, nil when absent).
//
// readBundleSig 读 sidecar（不存在返回 nil, nil）。
func readBundleSig(bundlePath string) (*nodeid.BundleSig, error) {
	raw, err := os.ReadFile(bundlePath + `.sig`)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sig nodeid.BundleSig
	if err := json.Unmarshal(raw, &sig); err != nil {
		return nil, fmt.Errorf(`.sig 解析失败: %w`, err)
	}
	return &sig, nil
}

// verifyBundleForImport applies the trust verdict to the import flow. SigInvalid
// and (team-mode) SigRejected are hard errors; unknown signer warns; verified notes.
//
// verifyBundleForImport 把信任判定应用到导入流程。SigInvalid 与（团队档）
// SigRejected 硬错误；未知签名者告警；verified 记录。
func verifyBundleForImport(bundlePath, digestHex string, out io.Writer) error {
	sig, err := readBundleSig(bundlePath)
	if err != nil {
		return err
	}
	ts, err := nodeid.LoadTrustStore()
	if err != nil {
		return err
	}
	switch ts.VerifyBundleSig(digestHex, sig) {
	case nodeid.SigVerified:
		fmt.Fprintf(out, `签名验证通过（节点 %s）\n`, sig.NodeID)
	case nodeid.SigMissing:
		fmt.Fprintln(out, `提示：bundle 无签名 sidecar（个人档默认放行；forge trust require-signed on 后必须签名）`)
	case nodeid.SigUnknownSigner:
		fmt.Fprintf(out, `⚠ 签名者 %s 不在 trust store——按未签名处理（forge trust add 登记后可验真）\n`, sig.NodeID)
	case nodeid.SigInvalid:
		return fmt.Errorf(`bundle 签名验证失败（内容被篡改或签名者与 trust store 公钥不符）——拒绝导入`)
	case nodeid.SigRejected:
		return fmt.Errorf(`团队档（require-signed）拒绝：bundle 缺失有效签名或签名者未登记`)
	}
	return nil
}
