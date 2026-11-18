-- 安装链接支持再次查看：令牌加密保存（与两步验证密钥同一把加密密钥）。此前生成的链接没有密文，无法再次查看。
ALTER TABLE repo_links ADD COLUMN token_enc VARCHAR(255) NULL COMMENT '加密保存的令牌，用于再次查看地址';
