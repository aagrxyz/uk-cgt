package statements

import (
	"fmt"

	"aagr.xyz/trades/parser"
	pb "aagr.xyz/trades/proto/statementspb"
	"aagr.xyz/trades/record"
)

func FromProtoConfig(cfgs *pb.Statements) ([]*Statement, error) {
	var res []*Statement
	for _, cfg := range cfgs.GetStatements() {
		p, err := parserFromProto(cfg)
		if err != nil {
			return nil, fmt.Errorf("cannot make a parser: %v", err)
		}
		res = append(res, New(p, cfg.GetDirectory(), cfg.GetFilenames()))
	}
	return res, nil
}

func parserFromProto(cfg *pb.Statement) (parser.Parser, error) {
	switch pCfg := cfg.ParserOneof.(type) {
	case *pb.Statement_DefaultParser:
		return parser.NewDefault(), nil
	case *pb.Statement_IbkrParser:
		act, err := record.AccountFromProto(pCfg.IbkrParser.GetAccount())
		if err != nil {
			return nil, fmt.Errorf("cannot parse account: %v", err)
		}
		return parser.NewIBKR(act)
	case *pb.Statement_IgDividendParser:
		act, err := record.AccountFromProto(pCfg.IgDividendParser.GetAccount())
		if err != nil {
			return nil, fmt.Errorf("cannot parse account: %v", err)
		}
		return parser.NewIGDividend(act), nil
	case *pb.Statement_IgParser:
		act, err := record.AccountFromProto(pCfg.IgParser.GetAccount())
		if err != nil {
			return nil, fmt.Errorf("cannot parse account: %v", err)
		}
		return parser.NewIG(act), nil
	case *pb.Statement_MsVestParser:
		act, err := record.AccountFromProto(pCfg.MsVestParser.GetAccount())
		if err != nil {
			return nil, fmt.Errorf("cannot parse account: %v", err)
		}
		return parser.NewMSVest(act)
	case *pb.Statement_MsWithdrawlParser:
		act, err := record.AccountFromProto(pCfg.MsWithdrawlParser.GetAccount())
		if err != nil {
			return nil, fmt.Errorf("cannot parse account: %v", err)
		}
		transferAct, err := record.AccountFromProto(pCfg.MsWithdrawlParser.GetWithdrawAccount())
		if err != nil {
			return nil, fmt.Errorf("cannot parse account: %v", err)
		}
		return parser.NewMSWithdraw(act, transferAct)
	case *pb.Statement_T212Parser:
		act, err := record.AccountFromProto(pCfg.T212Parser.GetAccount())
		if err != nil {
			return nil, fmt.Errorf("cannot parse account: %v", err)
		}
		return parser.NewT212(act), nil
	}
	return nil, fmt.Errorf("invalid type")
}
