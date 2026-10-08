import { App, Button, Card, Flex, Space, Tag, Typography } from "antd";

export function Workspace() {
  const { message } = App.useApp();
  return (
    <section className="workspace">
      <section className="workspace__hero">
        <Space direction="vertical" size="large">
          <Tag color="geekblue">Agent-native enterprise framework</Tag>
          <Typography.Title>WhereToLive Admin</Typography.Title>
          <Typography.Paragraph className="workspace__lead">
            面向 AI 协作的企业应用管理与快速开发工作台。
          </Typography.Paragraph>
          <Button
            type="primary"
            onClick={() => void message.info("工作台骨架已就绪")}
          >
            检查工作台
          </Button>
        </Space>
      </section>
      <Flex gap={16} wrap>
        <Card title="Contract first" className="workspace__card">
          HTTP 能力从同一份 OpenAPI 契约生成并验证。
        </Card>
        <Card title="Explicit modules" className="workspace__card">
          业务边界和依赖保持显式、稳定且可检查。
        </Card>
        <Card title="AI assisted" className="workspace__card">
          为可预测的 Agent 协作和快速交付而设计。
        </Card>
      </Flex>
    </section>
  );
}
