resource "aws_security_group_rule" "https" {
  security_group_id = aws_security_group.web.id
  type              = "ingress"
  from_port         = 443
  to_port           = 443
  protocol          = "tcp"
  cidr_blocks = [
    "10.0.0.0/8",     # office VPN
  ]
}
