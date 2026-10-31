import { Button } from "@/app/components/button";
import { Section } from "@/app/components/section";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";

// MockDescriptions is a static placeholder for the broker descriptions that
// await verification. It has no behaviour.
const rows = [
  {
    description: "VANGUARD FTSE ALL-WORLD UCITS ETF USD ACC",
    implies: "Vanguard FTSE All-World UCITS ETF",
    basis: "ISIN IE00BK5BQT80 on 2 keys; 1 key states the description alone",
  },
];

export function MockDescriptions() {
  return (
    <Section title="Descriptions to verify (mock)">
      <TableCard testId="mock-descriptions">
        <Thead>
          <tr>
            <Th>Description</Th>
            <Th>Implies</Th>
            <Th>Basis</Th>
            <Th> </Th>
          </tr>
        </Thead>
        <tbody>
          {rows.map((r) => (
            <Tr key={r.description}>
              <Td className="font-mono">{r.description}</Td>
              <Td>{r.implies}</Td>
              <Td className="text-text-muted">{r.basis}</Td>
              <Td>
                <Button variant="secondary" disabled>
                  Verify
                </Button>
              </Td>
            </Tr>
          ))}
        </tbody>
      </TableCard>
    </Section>
  );
}
