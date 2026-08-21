package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/MjxUpUp/Forge/internal/util"
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
	// Atomic write WITH fsync (util.AtomicWrite): a crash between the bundle rename
	// and a naive WriteFile could leave "new bundle + stale/truncated .sig", which
	// the import side hard-rejects (SigInvalid) in EVERY profile — and the earlier
	// hand-rolled version skipped the fsync its own comment promised.
	//
	// 带 fsync 的原子写（util.AtomicWrite）：bundle rename 与朴素 WriteFile 之间
	// 崩溃会留下「新 bundle + 旧/截断 .sig」组合，导入侧在任何档位都硬拒
	// （SigInvalid）——而此前手写版本恰恰漏了它注释里承诺的 fsync。
	if err := util.AtomicWrite(sigPath, raw, 0600); err != nil {
		return ``, err
	}
	return sigPath, nil
}

// writeBundleSigRespectingPolicy signs like writeBundleSig. err is non-nil ONLY for
// the hard case — team profile (RequireSigned) with a signing failure: pushing an
// unsigned bundle in team mode means your own peers (and your next pull) will reject
// it, so warn-and-continue is a silent sync break there. In personal profile a
// signing failure is soft (signed=false, err=nil) and callers warn.
//
// writeBundleSigRespectingPolicy 同 writeBundleSig 签名。err 非空仅对应硬情形——
// 团队档（RequireSigned）下签名失败：团队档推未签名的包等于推一个对端（和自己
// 下次 pull）都会拒的包，此时告警并继续就是静默破坏同步。个人档签名失败是软
// 性（signed=false, err=nil），调用方告警即可。
func writeBundleSigRespectingPolicy(bundlePath string) (sigPath string, signed bool, err error) {
	sigPath, err = writeBundleSig(bundlePath)
	if err == nil {
		return sigPath, true, nil
	}
	ts, tsErr := nodeid.LoadTrustStore()
	if tsErr == nil && ts.RequireSigned {
		return ``, false, fmt.Errorf(`团队档（require-signed）下 bundle 签名失败是硬错误: %w`, err)
	}
	// Soft path (personal profile, or an unreadable trust store where team mode
	// cannot be proven): the bundle travels unsigned, but the ROOT CAUSE must land
	// on stderr — the callers' generic warning cannot answer "why is it unsigned",
	// and silently treating an unreadable store as personal profile hides a
	// possible team-mode misconfiguration.
	//
	// 软路径（个人档，或 trust store 不可读、无法证明团队档）：bundle 无签名上路，
	// 但根因必须落到 stderr——调用方的通用告警回答不了「为什么没签名」，且把
	// 不可读的 store 无声按个人档处理会藏住团队档配置错误。
	profile := `个人档`
	if tsErr != nil {
		profile = `trust store 不可读，按个人档处理`
	}
	fmt.Fprintf(os.Stderr, `⚠ bundle 签名失败（%s）：%v`+"\n", profile, err)
	return ``, false, nil
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
		fmt.Fprintf(out, `签名验证通过（节点 %s）`+"\n", sig.NodeID)
	case nodeid.SigMissing:
		fmt.Fprintln(out, `提示：bundle 无签名 sidecar（个人档默认放行；forge trust require-signed on 后必须签名）`)
	case nodeid.SigUnknownSigner:
		fmt.Fprintf(out, `⚠ 签名者 %s 不在 trust store——按未签名处理（forge trust add 登记后可验真）`+"\n", sig.NodeID)
	case nodeid.SigInvalid:
		return fmt.Errorf(`bundle 签名验证失败（内容被篡改或签名者与 trust store 公钥不符）——拒绝导入`)
	case nodeid.SigRejected:
		return fmt.Errorf(`团队档（require-signed）拒绝：bundle 缺失有效签名或签名者未登记`)
	}
	return nil
}
