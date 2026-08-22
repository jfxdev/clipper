# Política de segurança

## Reportar uma vulnerabilidade

Use o [formulário privado de advisory do
GitHub](https://github.com/jfxdev/clipper/security/advisories/new). Ele cria
um canal privado entre você e os mantenedores.

**Não abra uma issue pública** para uma falha explorável — a issue fica
visível para todo mundo no instante em que é criada, inclusive para quem
quiser usá-la antes da correção.

O que ajuda em um relato:

- o que um atacante consegue fazer, e a partir de qual posição (visitante
  anônimo? quem já viu um link? quem controla o servidor?);
- passos para reproduzir, ou um proof-of-concept;
- versão/commit em que você observou o comportamento.

Retorno esperado: confirmação de recebimento em até 7 dias, e uma avaliação
inicial em até 30 dias. Este é um projeto mantido por voluntários — não há
programa de recompensa.

## Escopo

Está no escopo qualquer coisa que quebre uma destas propriedades:

1. **O servidor não consegue ler o conteúdo.** Todo o texto é cifrado no
   navegador (AES-256-GCM via WebCrypto); a chave viaja apenas no fragmento
   da URL (`#...`), que nunca é enviado ao servidor.
2. **Saber o ID de um paste não dá acesso a nada.** A leitura exige um
   *read token* — `base64url(SHA-256(fragmento))` — derivado da parte da URL
   que só quem tem o link completo possui.
3. **Saber o ID de um paste não permite destruí-lo.** O token é verificado
   dentro da mesma operação atômica que executa o burn-after-read, então uma
   tentativa com token errado não queima a mensagem.
4. **Nenhum script de terceiros executa na página.** A CSP é
   `default-src 'none'` com `script-src 'self'`, sem inline nem `eval`.
5. **Um paste expira.** Não existe retenção indefinida.

Execução de script na origem do clipper (XSS de qualquer tipo) é
**crítica**: a chave de decriptação está em `location.hash` e o texto claro
está no DOM.

## Fora do escopo (limites conhecidos do modelo)

Estes não são bugs — são propriedades do desenho, documentadas para que
ninguém confie no sistema além do que ele entrega:

- **Quem controla o servidor controla o JavaScript.** Criptografia no
  navegador, servida pelo mesmo servidor que guarda o texto cifrado, protege
  contra um vazamento do banco de dados e contra um operador curioso — não
  contra um servidor ativamente malicioso, que pode entregar um bundle que
  exfiltra o fragmento. As mitigações possíveis (CSP estrita, builds
  reprodutíveis com `-trimpath`, SBOM, imagem `scratch` pinada por digest)
  reduzem a superfície e tornam uma alteração detectável; nenhuma a elimina.
- **O link é a credencial.** Quem obtiver a URL completa lê a mensagem. É
  por isso que existem burn-after-read, expiração curta e a senha adicional.
- **Metadados são visíveis ao servidor:** ID, tamanho aproximado (o texto é
  preenchido em blocos de 256 bytes), horário de criação, se há senha, e o
  endereço IP de quem cria e de quem lê.
- **A senha adicional é atacável offline.** Quem tem o link baixa o texto
  cifrado e pode tentar senhas sem limite. Daí o PBKDF2 com 600.000
  iterações e o mínimo de 10 caracteres — nenhum dos dois substitui uma
  frase-senha de verdade.
